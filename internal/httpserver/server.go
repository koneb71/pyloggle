package httpserver

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/auth"
	"pyloggle/internal/config"
	"pyloggle/internal/ingest"
	"pyloggle/internal/metrics"
	"pyloggle/internal/otlp"
	"pyloggle/internal/storage/clickhouse"
	"pyloggle/internal/sysinfo"
	"pyloggle/internal/tenant"
	"pyloggle/internal/timeparse"
	"pyloggle/internal/webui"
)

// MetricsHandler serves the Prometheus text exposition; with a token it requires
// "Authorization: Bearer <token>".
func MetricsHandler(token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			auth := r.Header.Get("Authorization")
			if len(auth) < 8 || !strings.EqualFold(auth[:7], "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimSpace(auth[7:])), []byte(token)) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="pyloggle metrics"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		metrics.Default.WriteText(w)
	})
}

type Server struct {
	cfg config.Config
	ch  *clickhouse.Client
	mux *http.ServeMux
}

func New(cfg config.Config, ch *clickhouse.Client) *Server {
	s := &Server{
		cfg: cfg,
		ch:  ch,
		mux: http.NewServeMux(),
	}

	res := tenant.NewResolver(ch, 60*time.Second)
	ing := ingest.NewHandler(cfg, ch, res)
	startedAt := time.Now().UTC()
	collector := sysinfo.New(ch, time.Minute)
	go collector.Run(context.Background())

	// Prometheus exposition for the process and the ClickHouse gauges the collector refreshes.
	s.mux.Handle("GET /metrics", MetricsHandler(cfg.MetricsToken))

	// Best-effort: auto-create the default project (unlimited limits) so single-tenant/UI fallback works
	// even on a fresh database.
	if strings.TrimSpace(cfg.DefaultProjID) != "" {
		// ClickHouse may not be ready yet (common in docker-compose). Retry for a bit.
		go func(projectID string) {
			deadline := time.Now().Add(2 * time.Minute)
			backoff := 1 * time.Second
			for {
				if time.Now().After(deadline) {
					log.Printf("default project auto-create failed: deadline exceeded (gave up after 2m)")
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				_, err := ch.EnsureProject(ctx, clickhouse.ProjectUpsert{
					ID:              projectID,
					Name:            "Default",
					Description:     "Auto-created default project",
					MaxEventsPerSec: 0,
					MaxStorageGB:    0,
				})
				cancel()
				if err == nil {
					return
				}
				log.Printf("default project auto-create failed (will retry): %v", err)
				time.Sleep(backoff)
				if backoff < 10*time.Second {
					backoff *= 2
					if backoff > 10*time.Second {
						backoff = 10 * time.Second
					}
				}
			}
		}(strings.TrimSpace(cfg.DefaultProjID))
	}

	// Ingest endpoint
	// Note: ingest handler supports both multi-tenant API keys and (optional) legacy shared secret.
	s.mux.Handle("/v1/ingest", ing)

	// OpenTelemetry door: OTLP/HTTP logs and traces through the same pipeline. The standard
	// exporter paths (POST /v1/logs, /v1/traces, /v1/metrics) coexist with the GET query APIs.
	otlpH := otlp.New(ing.Pipeline(), cfg)
	for _, prefix := range []string{"/v1", "/v1/otlp/v1"} {
		s.mux.Handle("POST "+prefix+"/logs", otlpH.Logs())
		s.mux.Handle("POST "+prefix+"/traces", otlpH.Traces())
		s.mux.Handle("POST "+prefix+"/metrics", otlpH.Metrics())
	}

	// Health
	s.mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("verbose") != "" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "clickhouse": ch.Stats()})
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	// Minimal query endpoints
	s.mux.Handle("/v1/issues", auth.RequireProject(cfg, res, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query()
		projectID := auth.ProjectIDFromContext(r.Context())
		service := q.Get("service")
		env := q.Get("env")
		from := parseTime(q.Get("from"))
		to := parseTime(q.Get("to"))
		limit := parseInt(q.Get("limit"), 50)
		items, err := ch.ListIssues(r.Context(), projectID, service, env, from, to, limit)
		if err != nil {
			http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, items)
	})))

	s.mux.Handle("/v1/logs", auth.RequireProject(cfg, res, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query()
		projectID := auth.ProjectIDFromContext(r.Context())
		service := q.Get("service")
		env := q.Get("env")
		level := strings.TrimSpace(q.Get("level"))
		search := q.Get("q")
		traceID := q.Get("trace_id")
		requestID := q.Get("request_id")
		from := parseTime(q.Get("from"))
		to := parseTime(q.Get("to"))
		limit := parseInt(q.Get("limit"), 200)
		items, err := ch.ListLogs(r.Context(), projectID, service, env, level, search, traceID, requestID, from, to, limit)
		if err != nil {
			http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, items)
	})))

	s.mux.Handle("/v1/issues/occurrences", auth.RequireProject(cfg, res, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query()
		projectID := auth.ProjectIDFromContext(r.Context())
		issueID := q.Get("issue_id")
		if issueID == "" {
			http.Error(w, "missing issue_id", http.StatusBadRequest)
			return
		}
		from := parseTime(q.Get("from"))
		to := parseTime(q.Get("to"))
		limit := parseInt(q.Get("limit"), 50)
		items, err := ch.ListIssueOccurrences(r.Context(), projectID, issueID, from, to, limit)
		if err != nil {
			http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, items)
	})))

	// Issue lifecycle updates (admin only)
	s.mux.Handle("/v1/issues/state", auth.RequireAdminToken(cfg.AdminToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req api.IssueStateUpdate
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if req.ProjectID == "" {
			req.ProjectID = cfg.DefaultProjID
		}
		if req.ProjectID == "" {
			http.Error(w, "missing project_id", http.StatusBadRequest)
			return
		}
		if _, ok, err := ch.GetProjectByID(r.Context(), req.ProjectID); err != nil {
			http.Error(w, "project lookup failed", http.StatusInternalServerError)
			return
		} else if !ok {
			http.Error(w, "unknown project_id", http.StatusNotFound)
			return
		}
		if req.IssueID == "" || req.Service == "" || req.Env == "" {
			http.Error(w, "missing issue_id/service/env", http.StatusBadRequest)
			return
		}
		st := strings.ToLower(req.Status)
		if st != "open" && st != "resolved" && st != "ignored" {
			http.Error(w, "status must be open|resolved|ignored", http.StatusBadRequest)
			return
		}
		if err := ch.UpsertIssueState(r.Context(), req.ProjectID, req.Service, req.Env, req.IssueID, st, req.UpdatedBy, req.Comment); err != nil {
			http.Error(w, "update failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"ok": true})
	})))

	// Alert rules (admin only)
	s.mux.Handle("/v1/alerts/rules", auth.RequireAdminToken(cfg.AdminToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			q := r.URL.Query()
			projectID := q.Get("project_id")
			if projectID == "" {
				projectID = cfg.DefaultProjID
			}
			if projectID == "" {
				http.Error(w, "missing project_id", http.StatusBadRequest)
				return
			}
			if _, ok, err := ch.GetProjectByID(r.Context(), projectID); err != nil {
				http.Error(w, "project lookup failed", http.StatusInternalServerError)
				return
			} else if !ok {
				http.Error(w, "unknown project_id", http.StatusNotFound)
				return
			}
			items, err := ch.ListAlertRules(r.Context(), projectID)
			if err != nil {
				http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, items)
			return
		case http.MethodPost:
			var req api.AlertRuleUpsertRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			rule := req.Rule
			if rule.ProjectID == "" {
				rule.ProjectID = cfg.DefaultProjID
			}
			if rule.ProjectID == "" {
				http.Error(w, "missing project_id", http.StatusBadRequest)
				return
			}
			if _, ok, err := ch.GetProjectByID(r.Context(), rule.ProjectID); err != nil {
				http.Error(w, "project lookup failed", http.StatusInternalServerError)
				return
			} else if !ok {
				http.Error(w, "unknown project_id", http.StatusNotFound)
				return
			}
			rule, err := ch.UpsertAlertRule(r.Context(), rule)
			if err != nil {
				http.Error(w, "upsert failed", http.StatusInternalServerError)
				return
			}
			writeJSON(w, rule)
			return
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})))

	// Saved searches (admin only for mutating)
	s.mux.Handle("/v1/saved-searches", auth.RequireProject(cfg, res, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			q := r.URL.Query()
			projectID := auth.ProjectIDFromContext(r.Context())
			userID := q.Get("user_id")
			includePublic := parseBool(q.Get("include_public"), true)
			onlyPinned := parseBool(q.Get("only_pinned"), false)
			limit := parseInt(q.Get("limit"), 50)

			items, err := ch.ListSavedSearches(r.Context(), projectID, userID, includePublic, onlyPinned, limit)
			if err != nil {
				http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, items)
			return

		case http.MethodPost:
			// admin-gated
			if !checkAdmin(cfg.AdminToken, r) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			var req api.SavedSearchUpsertRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			srch := req.Search
			if srch.ProjectID == "" {
				srch.ProjectID = cfg.DefaultProjID
			}
			if srch.ProjectID == "" {
				http.Error(w, "missing project_id", http.StatusBadRequest)
				return
			}
			if _, ok, err := ch.GetProjectByID(r.Context(), srch.ProjectID); err != nil {
				http.Error(w, "project lookup failed", http.StatusInternalServerError)
				return
			} else if !ok {
				http.Error(w, "unknown project_id", http.StatusNotFound)
				return
			}
			out, err := ch.UpsertSavedSearch(r.Context(), srch)
			if err != nil {
				http.Error(w, "upsert failed", http.StatusBadRequest)
				return
			}
			writeJSON(w, out)
			return

		case http.MethodDelete:
			// admin-gated
			if !checkAdmin(cfg.AdminToken, r) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			q := r.URL.Query()
			projectID := q.Get("project_id")
			if projectID == "" {
				projectID = cfg.DefaultProjID
			}
			if projectID == "" {
				http.Error(w, "missing project_id", http.StatusBadRequest)
				return
			}
			if _, ok, err := ch.GetProjectByID(r.Context(), projectID); err != nil {
				http.Error(w, "project lookup failed", http.StatusInternalServerError)
				return
			} else if !ok {
				http.Error(w, "unknown project_id", http.StatusNotFound)
				return
			}
			id := q.Get("id")
			userID := q.Get("user_id")
			if id == "" || userID == "" {
				http.Error(w, "missing id/user_id", http.StatusBadRequest)
				return
			}
			if err := ch.SoftDeleteSavedSearch(r.Context(), projectID, userID, id); err != nil {
				http.Error(w, "delete failed", http.StatusInternalServerError)
				return
			}
			writeJSON(w, map[string]any{"ok": true})
			return

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})))

	// Basic dashboard metric: error trend time series
	s.mux.Handle("/v1/metrics/errors/trend", auth.RequireProject(cfg, res, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		q := r.URL.Query()
		projectID := auth.ProjectIDFromContext(r.Context())
		service := q.Get("service")
		env := q.Get("env")
		httpRoute := q.Get("http_route")
		httpMethod := q.Get("http_method")
		from := parseTime(q.Get("from"))
		to := parseTime(q.Get("to"))
		bucket := parseInt(q.Get("bucket_seconds"), 60)

		items, err := ch.ErrorTrend(r.Context(), projectID, service, env, httpRoute, httpMethod, from, to, bucket)
		if err != nil {
			http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, items)
	})))

	// Project management (admin only)
	s.mux.Handle("/v1/projects", auth.RequireAdminToken(cfg.AdminToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			limit := parseInt(r.URL.Query().Get("limit"), 200)
			items, err := ch.ListProjects(r.Context(), limit)
			if err != nil {
				http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			// Map to API type (keeps JSON stable at the edge).
			out := make([]api.Project, 0, len(items))
			for _, p := range items {
				out = append(out, api.Project{
					ID:              p.ID,
					Name:            p.Name,
					Description:     p.Description,
					MaxEventsPerSec: p.MaxEventsPerSec,
					MaxStorageGB:    p.MaxStorageGB,
					APIKeyHash:      p.APIKeyHash,
					APIKeyCreatedAt: p.APIKeyCreatedAt,
					CreatedAt:       p.CreatedAt,
					UpdatedAt:       p.UpdatedAt,
				})
			}
			writeJSON(w, out)
			return

		case http.MethodPost:
			var req api.ProjectCreateRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			p, apiKey, err := ch.CreateProject(r.Context(), clickhouse.ProjectUpsert{
				ID:              req.ID,
				Name:            req.Name,
				Description:     req.Description,
				MaxEventsPerSec: req.MaxEventsPerSec,
				MaxStorageGB:    req.MaxStorageGB,
			})
			if err != nil {
				http.Error(w, "create failed", http.StatusBadRequest)
				return
			}
			writeJSON(w, api.ProjectCreateResponse{
				Project: api.Project{
					ID:              p.ID,
					Name:            p.Name,
					Description:     p.Description,
					MaxEventsPerSec: p.MaxEventsPerSec,
					MaxStorageGB:    p.MaxStorageGB,
					APIKeyHash:      p.APIKeyHash,
					APIKeyCreatedAt: p.APIKeyCreatedAt,
					CreatedAt:       p.CreatedAt,
					UpdatedAt:       p.UpdatedAt,
				},
				APIKey: apiKey,
			})
			return

		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})))

	s.mux.Handle("/v1/projects/update", auth.RequireAdminToken(cfg.AdminToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req api.ProjectUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		p, err := ch.UpsertProject(r.Context(), clickhouse.ProjectUpsert{
			ID:              req.ID,
			Name:            req.Name,
			Description:     req.Description,
			MaxEventsPerSec: req.MaxEventsPerSec,
			MaxStorageGB:    req.MaxStorageGB,
		})
		if err != nil {
			http.Error(w, "update failed", http.StatusBadRequest)
			return
		}
		writeJSON(w, api.Project{
			ID:              p.ID,
			Name:            p.Name,
			Description:     p.Description,
			MaxEventsPerSec: p.MaxEventsPerSec,
			MaxStorageGB:    p.MaxStorageGB,
			APIKeyHash:      p.APIKeyHash,
			APIKeyCreatedAt: p.APIKeyCreatedAt,
			CreatedAt:       p.CreatedAt,
			UpdatedAt:       p.UpdatedAt,
		})
	})))

	s.mux.Handle("/v1/projects/rotate-key", auth.RequireAdminToken(cfg.AdminToken, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req api.ProjectRotateKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		p, apiKey, err := ch.RotateProjectAPIKey(r.Context(), req.ID)
		if err != nil {
			http.Error(w, "rotate failed", http.StatusBadRequest)
			return
		}
		writeJSON(w, api.ProjectRotateKeyResponse{
			Project: api.Project{
				ID:              p.ID,
				Name:            p.Name,
				Description:     p.Description,
				MaxEventsPerSec: p.MaxEventsPerSec,
				MaxStorageGB:    p.MaxStorageGB,
				APIKeyHash:      p.APIKeyHash,
				APIKeyCreatedAt: p.APIKeyCreatedAt,
				CreatedAt:       p.CreatedAt,
				UpdatedAt:       p.UpdatedAt,
			},
			APIKey: apiKey,
		})
	})))

	// Web UI + its JSON API (session cookies), mirrored under /v1/q/ for API-key clients.
	ui := webui.New(cfg, ch, res, ing.Pipeline().States()).WithSystem(collector, startedAt)
	s.mux.Handle("/ui/", ui)
	s.mux.Handle("/v1/q/", ui.APIKeyAPIHandler())
	s.mux.HandleFunc("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})
	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Only handle "bare" root; let other specific routes win.
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})

	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}

func parseInt(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func parseBool(s string, def bool) bool {
	if s == "" {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "t", "true", "y", "yes", "on":
		return true
	case "0", "f", "false", "n", "no", "off":
		return false
	default:
		return def
	}
}

func parseTime(s string) time.Time {
	t, _ := timeparse.Parse(s)
	return t
}

func checkAdmin(adminToken string, r *http.Request) bool {
	if adminToken == "" {
		return false
	}
	key := r.Header.Get("X-Admin-Token")
	return key != "" && subtle.ConstantTimeCompare([]byte(key), []byte(adminToken)) == 1
}
