package webui

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pyloggle/internal/auth"
	"pyloggle/internal/logquery"
	"pyloggle/internal/tenant"
	"pyloggle/internal/timeparse"
)

// The JSON API consumed by the SPA lives under /ui/api/ (session cookies) and is mirrored under
// /v1/q/ (API key) for scripts. Handlers only read the project from the request context and
// the user from the UI session, so the same mux serves both mounts.

const csrfHeader = "X-Pyloggle-Request"

type apiError struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
	Pos     int    `json:"pos,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Error: code, Message: msg})
}

func writeQueryErr(w http.ResponseWriter, err error) {
	var qe *logquery.Error
	if errors.As(err, &qe) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad_query", "message": qe.Msg, "pos": qe.Pos})
		return
	}
	writeErr(w, http.StatusBadRequest, "bad_query", err.Error())
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	return dec.Decode(v)
}

func qInt(r *http.Request, key string, def, max int) int {
	s := strings.TrimSpace(r.URL.Query().Get(key))
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return def
	}
	if max > 0 && n > max {
		return max
	}
	return n
}

// qRange parses from/to with a default window; "to" defaults to now.
func qRange(r *http.Request, def time.Duration) (time.Time, time.Time) {
	q := r.URL.Query()
	return timeparse.ParseRange(q.Get("from"), q.Get("to"), def, time.Now().UTC())
}

func qTime(r *http.Request, key string) time.Time {
	t, _ := timeparse.Parse(r.URL.Query().Get(key))
	return t
}

func qStrings(r *http.Request, key string) []string {
	var out []string
	for _, v := range r.URL.Query()[key] {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// newAPIMux registers every API route under prefix (e.g. "/ui/api" or "/v1/q").
func (u *UI) newAPIMux(prefix string) *http.ServeMux {
	mux := http.NewServeMux()
	reg := func(method, path string, h http.HandlerFunc) {
		mux.HandleFunc(method+" "+prefix+path, h)
	}
	// session / project
	reg("GET", "/me", u.apiMe)
	reg("POST", "/login", u.apiLogin)
	reg("POST", "/logout", u.apiLogout)
	reg("POST", "/project/select", u.apiSelectProject)
	// issues
	reg("GET", "/issues", u.apiIssues)
	reg("POST", "/issues/state", u.apiIssuesBulkState)
	reg("GET", "/issues/assignees", u.apiIssueAssignees)
	reg("POST", "/issues/{id}/assign", u.apiIssueAssign)
	reg("POST", "/issues/{id}/comments", u.apiIssueComment)
	reg("GET", "/issues/{id}/activity", u.apiIssueActivity)
	reg("GET", "/issues/{id}", u.apiIssue)
	reg("GET", "/issues/{id}/trend", u.apiIssueTrend)
	reg("GET", "/issues/{id}/breakdown", u.apiIssueBreakdown)
	reg("GET", "/issues/{id}/occurrences", u.apiIssueOccurrences)
	reg("GET", "/issues/{id}/occurrences/latest", u.apiIssueLatest)
	reg("GET", "/issues/{id}/occurrences/{event_id}", u.apiOccurrence)
	reg("POST", "/issues/{id}/state", u.apiIssueState)
	reg("GET", "/events/{event_id}", u.apiOccurrence)
	// logs
	reg("GET", "/logs", u.apiLogs)
	reg("GET", "/logs/histogram", u.apiLogsHistogram)
	reg("GET", "/logs/facets", u.apiLogsFacets)
	reg("GET", "/logs/context", u.apiLogsContext)
	reg("GET", "/logs/patterns", u.apiLogsPatterns)
	reg("GET", "/logs/fields", u.apiLogsFields)
	reg("GET", "/saved-views", u.apiSavedViews)
	reg("POST", "/saved-views", u.apiSavedViewUpsert)
	reg("DELETE", "/saved-views/{id}", u.apiSavedViewDelete)
	// metrics
	reg("GET", "/metrics/overview", u.apiOverview)
	reg("GET", "/metrics/errors", u.apiErrorTrend)
	reg("GET", "/metrics/logs", u.apiLogVolume)
	reg("GET", "/metrics/top-issues", u.apiTopIssues)
	// projects
	reg("GET", "/projects", u.apiProjects)
	reg("POST", "/projects", u.apiProjectCreate)
	reg("POST", "/projects/{id}", u.apiProjectUpdate)
	reg("POST", "/projects/{id}/rotate-key", u.apiProjectRotateKey)
	// alerts
	reg("GET", "/alerts/rules", u.apiAlertRules)
	reg("POST", "/alerts/rules", u.apiAlertRuleUpsert)
	reg("GET", "/alerts/rules/preview", u.apiAlertRulePreview)
	reg("GET", "/alerts/firings", u.apiAlertFirings)
	return mux
}

// uiAPIHandler wraps the /ui/api mux with session, CSRF and project resolution.
func (u *UI) uiAPIHandler() http.Handler {
	mux := u.newAPIMux("/ui/api")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isMutation(r.Method) && !csrfOK(r) {
			writeErr(w, http.StatusForbidden, "csrf", "missing "+csrfHeader+" header")
			return
		}
		u.requireUISessionAPI(u.withProjectAPI(mux)).ServeHTTP(w, r)
	})
}

// APIKeyAPIHandler mounts the same routes under /v1/q for API-key clients: X-API-Key (or
// Authorization: Bearer) selects the project; without a key the default project applies to
// reads only.
func (u *UI) APIKeyAPIHandler() http.Handler {
	mux := u.newAPIMux("/v1/q")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.Header.Get("X-API-Key"))
		if key == "" {
			if a := r.Header.Get("Authorization"); len(a) > 7 && strings.EqualFold(a[:7], "Bearer ") {
				key = strings.TrimSpace(a[7:])
			}
		}
		var p tenant.Project
		switch {
		case key != "":
			if u.keys == nil {
				writeErr(w, http.StatusUnauthorized, "unauthorized", "unknown API key")
				return
			}
			proj, ok, err := u.keys.ResolveByAPIKey(r.Context(), key)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, "lookup_failed", err.Error())
				return
			}
			if !ok {
				writeErr(w, http.StatusUnauthorized, "unauthorized", "unknown API key")
				return
			}
			p = proj
		case isMutation(r.Method):
			writeErr(w, http.StatusUnauthorized, "api_key_required", "mutations need an explicit X-API-Key")
			return
		default:
			def := strings.TrimSpace(u.cfg.DefaultProjID)
			if def == "" {
				writeErr(w, http.StatusUnauthorized, "unauthorized", "X-API-Key required")
				return
			}
			pv, ok, err := u.store.GetProjectByID(r.Context(), def)
			if err != nil || !ok {
				writeErr(w, http.StatusUnauthorized, "unauthorized", "default project unavailable")
				return
			}
			p = tenant.Project{ID: pv.ID, Name: pv.Name}
		}
		mux.ServeHTTP(w, r.WithContext(auth.WithProject(r.Context(), p)))
	})
}

func isMutation(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// csrfOK requires the custom header (browsers cannot add it cross-origin without CORS) and,
// when the browser reports it, a same-origin fetch.
func csrfOK(r *http.Request) bool {
	if strings.TrimSpace(r.Header.Get(csrfHeader)) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
	case "", "same-origin", "none":
		return true
	}
	return false
}

// requireUISessionAPI gates the JSON API on the UI session (JSON 401 instead of a redirect).
func (u *UI) requireUISessionAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mode := strings.ToLower(strings.TrimSpace(u.cfg.UIAuthMode))
		if mode == "disabled" || mode == "" {
			next.ServeHTTP(w, r)
			return
		}
		if claims, ok := u.getUISession(r); ok {
			next.ServeHTTP(w, r.WithContext(withUIUser(r.Context(), claims)))
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "login_required", "login_url": "/ui/login"})
	})
}

// withProjectAPI resolves the active project like the HTML flow (query param, project cookie,
// API key header/cookie, default project) but answers 401 JSON when none applies. Routes that
// work without a project (me, login, logout, project select, projects list) still run.
func (u *UI) withProjectAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, status, err := u.resolveProject(r)
		if err != nil {
			if projectOptional(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			writeErr(w, status, "no_project", err.Error())
			return
		}
		if pid := strings.TrimSpace(r.URL.Query().Get("project_id")); pid != "" {
			u.setUIProjectID(w, p.ID)
		}
		next.ServeHTTP(w, r.WithContext(auth.WithProject(r.Context(), p)))
	})
}

func projectOptional(path string) bool {
	for _, suffix := range []string{"/me", "/login", "/logout", "/project/select", "/projects"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

var errNoProject = errors.New("no project selected")

func (u *UI) resolveProject(r *http.Request) (tenant.Project, int, error) {
	ctx := r.Context()
	lookup := func(id string) (tenant.Project, bool, error) {
		pv, ok, err := u.store.GetProjectByID(ctx, id)
		if err != nil || !ok || pv.ID == "" {
			return tenant.Project{}, false, err
		}
		return tenant.Project{ID: pv.ID, Name: pv.Name, MaxEventsPerSec: pv.MaxEventsPerSec, MaxStorageGB: pv.MaxStorageGB}, true, nil
	}
	if pid := strings.TrimSpace(r.URL.Query().Get("project_id")); pid != "" {
		p, ok, err := lookup(pid)
		if err != nil {
			return p, http.StatusInternalServerError, err
		}
		if !ok {
			return p, http.StatusBadRequest, errors.New("unknown project_id")
		}
		return p, 0, nil
	}
	if pid := strings.TrimSpace(u.getUIProjectID(r)); pid != "" {
		if p, ok, err := lookup(pid); err == nil && ok {
			return p, 0, nil
		}
	}
	apiKey := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if apiKey == "" {
		if c, err := r.Cookie(apiKeyCookie); err == nil {
			apiKey = strings.TrimSpace(c.Value)
		}
	}
	if apiKey != "" && u.keys != nil {
		if p, ok, err := u.keys.ResolveByAPIKey(ctx, apiKey); err == nil && ok {
			return p, 0, nil
		}
	}
	if def := strings.TrimSpace(u.cfg.DefaultProjID); def != "" {
		if p, ok, err := lookup(def); err == nil && ok {
			return p, 0, nil
		}
	}
	return tenant.Project{}, http.StatusUnauthorized, errNoProject
}

// currentUser names the actor for audit fields.
func currentUser(r *http.Request) string {
	if c, ok := uiUserFromContext(r.Context()); ok {
		if c.Email != "" {
			return c.Email
		}
		if c.PreferredUsername != "" {
			return c.PreferredUsername
		}
		return c.Sub
	}
	if strings.TrimSpace(r.Header.Get("X-API-Key")) != "" {
		return "api"
	}
	return "ui"
}

// isUIAdmin gates project/alert mutations when PYLOGGLE_UI_ADMINS is set (Azure mode only).
func (u *UI) isUIAdmin(r *http.Request) bool {
	if len(u.cfg.UIAdmins) == 0 {
		return true
	}
	mode := strings.ToLower(strings.TrimSpace(u.cfg.UIAuthMode))
	if mode == "disabled" || mode == "" {
		return true
	}
	c, ok := uiUserFromContext(r.Context())
	if !ok {
		return false
	}
	for _, a := range u.cfg.UIAdmins {
		a = strings.ToLower(strings.TrimSpace(a))
		if a == "" {
			continue
		}
		if a == strings.ToLower(c.Email) || a == strings.ToLower(c.PreferredUsername) || a == strings.ToLower(c.Sub) {
			return true
		}
	}
	return false
}
