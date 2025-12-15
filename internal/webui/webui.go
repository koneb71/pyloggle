package webui

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/auth"
	"pyloggle/internal/config"
	"pyloggle/internal/storage/clickhouse"
	"pyloggle/internal/tenant"
)

//go:embed templates
var templatesFS embed.FS

const apiKeyCookie = "pyloggle_api_key"
const uiProjectIDCookie = "pyloggle_ui_project_id"

type UI struct {
	cfg  config.Config
	ch   *clickhouse.Client
	res  *tenant.Resolver
	tpl  map[string]*template.Template
	jwks *jwksCache
}

func isHXRequest(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.Header.Get("HX-Request")), "true")
}

func (u *UI) renderPageOrPartial(w http.ResponseWriter, r *http.Request, page string, data any) {
	// Make sure caches/CDNs don't mix full vs partial responses.
	w.Header().Add("Vary", "HX-Request")

	name := page
	if isHXRequest(r) {
		// Each page template defines a "content" template; rendering it yields a fragment suitable for HTMX swaps.
		name = "content"
	}
	_ = u.tpl[page].ExecuteTemplate(w, name, data)
}

func New(cfg config.Config, ch *clickhouse.Client, res *tenant.Resolver) http.Handler {
	funcs := template.FuncMap{
		"formatTS": func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.UTC().Format(time.RFC3339)
		},
		"formatDTLocal": func(s string) string {
			// Render a datetime-local value in UTC when possible.
			// If the incoming value isn't parseable, return "" so the control doesn't show a misleading value.
			t := parseTimeUI(s)
			if t.IsZero() {
				return ""
			}
			return t.UTC().Format("2006-01-02T15:04")
		},
		"short": func(s string, n int) string {
			s = strings.TrimSpace(s)
			if n <= 0 || len(s) <= n {
				return s
			}
			return s[:n]
		},
		"badgeClass": func(status string, regressed bool) string {
			s := strings.ToLower(strings.TrimSpace(status))
			if regressed {
				return "badge text-bg-danger"
			}
			switch s {
			case "resolved":
				return "badge text-bg-success"
			case "ignored":
				return "badge text-bg-secondary"
			default:
				return "badge text-bg-primary"
			}
		},
		"urlq": func(s string) string { return url.QueryEscape(s) },
		"trim": func(s string) string { return strings.TrimSpace(s) },
	}

	parsePage := func(page string) *template.Template {
		return template.Must(template.New("base.html").Funcs(funcs).ParseFS(
			templatesFS,
			"templates/base.html",
			"templates/"+page,
		))
	}
	tpls := map[string]*template.Template{
		"login.html":        parsePage("login.html"),
		"issues.html":       parsePage("issues.html"),
		"issue_detail.html": parsePage("issue_detail.html"),
		"logs.html":         parsePage("logs.html"),
		"dashboards.html":   parsePage("dashboards.html"),
		"projects.html":     parsePage("projects.html"),
	}
	return &UI{cfg: cfg, ch: ch, res: res, tpl: tpls}
}

type baseData struct {
	Title      string
	Project    tenant.Project
	Projects   []clickhouse.ProjectView
	CurrentURI string
	Now        time.Time
	Error      string
	PollSeconds int
}

type issuesData struct {
	baseData
	Service string
	Env     string
	Status  string
	From    string
	To      string
	Limit   int
	Issues  []api.IssueSummary
}

type frame struct {
	Filename string
	Function string
	Lineno   uint32
	Context  string
	IsInApp  bool
}

type issueDetailData struct {
	baseData
	IssueID      string
	Issue        *api.IssueSummary
	From         string
	To           string
	Limit        int
	Back         string
	Occurrences  []api.ErrorOccurrence
	LatestFrames []frame
}

type logsData struct {
	baseData
	Service   string
	Env       string
	Level     string
	Q         string
	TraceID   string
	RequestID string
	From      string
	To        string
	Limit     int
	Cursors   []string
	NewerURL  string
	OlderURL  string
	HasNewer  bool
	HasOlder  bool
	Events    []api.LogEvent
}

type dashboardsData struct {
	baseData
	Service string
	Env     string
	From    string
	To      string
	Bucket  int
	Trend   []api.TimeSeriesPoint
	SVG     template.HTML
}

func (u *UI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only handle /ui/* subtree.
	if !strings.HasPrefix(r.URL.Path, "/ui/") {
		http.NotFound(w, r)
		return
	}

	// Routes that do NOT require a project context (API key cookie), but do require a UI session when enabled.
	switch strings.TrimSuffix(r.URL.Path, "/") {
	case "/ui/projects":
		u.requireUISession(http.HandlerFunc(u.projectsList)).ServeHTTP(w, r)
		return
	case "/ui/projects/create":
		u.requireUISession(http.HandlerFunc(u.projectsCreate)).ServeHTTP(w, r)
		return
	case "/ui/projects/update":
		u.requireUISession(http.HandlerFunc(u.projectsUpdate)).ServeHTTP(w, r)
		return
	case "/ui/projects/rotate-key":
		u.requireUISession(http.HandlerFunc(u.projectsRotateKey)).ServeHTTP(w, r)
		return
	case "/ui/select-project":
		u.requireUISession(http.HandlerFunc(u.selectProject)).ServeHTTP(w, r)
		return
	}

	switch {
	case r.URL.Path == "/ui/" || r.URL.Path == "/ui":
		http.Redirect(w, r, "/ui/issues", http.StatusFound)
		return
	case r.URL.Path == "/ui/login":
		u.login(w, r)
		return
	case r.URL.Path == "/ui/oauth2/callback":
		// Azure auth callback (when enabled).
		u.azureCallback(w, r)
		return
	case r.URL.Path == "/ui/logout":
		u.logout(w, r)
		return
	}

	// Everything else requires UI auth (when enabled) + a project context (API key cookie or default project).
	h := u.withProject(http.HandlerFunc(u.authed))
	u.requireUISession(h).ServeHTTP(w, r)
}

func (u *UI) authed(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/ui/issues":
		u.issues(w, r)
		return
	case strings.HasPrefix(r.URL.Path, "/ui/issues/"):
		u.issueDetail(w, r)
		return
	case r.URL.Path == "/ui/logs":
		u.logs(w, r)
		return
	case r.URL.Path == "/ui/dashboards":
		u.dashboards(w, r)
		return
	default:
		http.NotFound(w, r)
		return
	}
}

func (u *UI) requireUISession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mode := strings.ToLower(strings.TrimSpace(u.cfg.UIAuthMode))
		if mode == "disabled" || mode == "" {
			next.ServeHTTP(w, r)
			return
		}
		// Public UI endpoints.
		switch r.URL.Path {
		case "/ui/login", "/ui/logout", "/ui/oauth2/callback":
			next.ServeHTTP(w, r)
			return
		}
		if claims, ok := u.getUISession(r); ok {
			ctx := withUIUser(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		returnTo := r.URL.RequestURI()
		http.Redirect(w, r, "/ui/login?next="+url.QueryEscape(returnTo), http.StatusFound)
	})
}

func (u *UI) withProject(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prefer explicit / persisted project_id selection for the UI (supports multi-project viewing
		// without requiring raw API keys on every switch).
		if pid := strings.TrimSpace(r.URL.Query().Get("project_id")); pid != "" {
			pv, ok, err := u.ch.GetProjectByID(r.Context(), pid)
			if err != nil {
				http.Error(w, "project lookup failed", http.StatusInternalServerError)
				return
			}
			if !ok || pv.ID == "" {
				http.Error(w, "unknown project_id", http.StatusBadRequest)
				return
			}
			u.setUIProjectID(w, pv.ID)
			projCtx := auth.WithProject(r.Context(), tenant.Project{ID: pv.ID, Name: pv.Name})
			next.ServeHTTP(w, r.WithContext(projCtx))
			return
		}
		if pid := strings.TrimSpace(u.getUIProjectID(r)); pid != "" {
			pv, ok, err := u.ch.GetProjectByID(r.Context(), pid)
			if err != nil {
				http.Error(w, "project lookup failed", http.StatusInternalServerError)
				return
			}
			if ok && pv.ID != "" {
				ctx := auth.WithProject(r.Context(), tenant.Project{ID: pv.ID, Name: pv.Name})
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			// Stale selection: clear and fall through to legacy selection mechanisms.
			u.clearUIProjectID(w)
		}

		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			if c, err := r.Cookie(apiKeyCookie); err == nil {
				apiKey = strings.TrimSpace(c.Value)
			}
		}

		// Local-dev bypass: if UI auth is disabled and no API key is provided, fall back to DefaultProjID
		// without forcing a login redirect.
		if strings.EqualFold(strings.TrimSpace(u.cfg.UIAuthMode), "disabled") && strings.TrimSpace(apiKey) == "" {
			if strings.TrimSpace(u.cfg.DefaultProjID) == "" {
				http.Error(w, "UI auth is disabled but no default project is configured (set PYLOGGLE_DEFAULT_PROJECT_ID)", http.StatusInternalServerError)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			pv, ok, err := u.ch.GetProjectByID(ctx, u.cfg.DefaultProjID)
			if err != nil {
				http.Error(w, "project lookup failed", http.StatusInternalServerError)
				return
			}
			if !ok || pv.ID == "" {
				_, ensureErr := u.ch.EnsureProject(ctx, clickhouse.ProjectUpsert{
					ID:              u.cfg.DefaultProjID,
					Name:            "Default",
					Description:     "Auto-created default project",
					MaxEventsPerSec: 0,
					MaxStorageGB:    0,
				})
				if ensureErr != nil {
					// Most common failure mode is ClickHouse not ready yet.
					if errors.Is(ensureErr, context.DeadlineExceeded) || errors.Is(ensureErr, context.Canceled) {
						http.Error(w, "default project auto-create failed (clickhouse not ready): "+ensureErr.Error(), http.StatusServiceUnavailable)
						return
					}
					http.Error(w, "default project auto-create failed: "+ensureErr.Error(), http.StatusInternalServerError)
					return
				}
				pv, ok, err = u.ch.GetProjectByID(ctx, u.cfg.DefaultProjID)
				if err != nil {
					http.Error(w, "project lookup failed", http.StatusInternalServerError)
					return
				}
				if !ok || pv.ID == "" {
					http.Error(w, "default project not found", http.StatusInternalServerError)
					return
				}
			}
			projCtx := auth.WithProject(r.Context(), tenant.Project{ID: pv.ID, Name: pv.Name})
			next.ServeHTTP(w, r.WithContext(projCtx))
			return
		}

		// Single-tenant mode fallback.
		if apiKey == "" && u.cfg.DefaultProjID != "" {
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			defer cancel()
			pv, ok, err := u.ch.GetProjectByID(ctx, u.cfg.DefaultProjID)
			if err != nil {
				http.Error(w, "project lookup failed", http.StatusInternalServerError)
				return
			}
			if !ok || pv.ID == "" {
				_, ensureErr := u.ch.EnsureProject(ctx, clickhouse.ProjectUpsert{
					ID:              u.cfg.DefaultProjID,
					Name:            "Default",
					Description:     "Auto-created default project",
					MaxEventsPerSec: 0,
					MaxStorageGB:    0,
				})
				if ensureErr != nil {
					if errors.Is(ensureErr, context.DeadlineExceeded) || errors.Is(ensureErr, context.Canceled) {
						http.Error(w, "default project auto-create failed (clickhouse not ready): "+ensureErr.Error(), http.StatusServiceUnavailable)
						return
					}
					http.Error(w, "default project auto-create failed: "+ensureErr.Error(), http.StatusInternalServerError)
					return
				}
				pv, ok, err = u.ch.GetProjectByID(ctx, u.cfg.DefaultProjID)
				if err != nil {
					http.Error(w, "project lookup failed", http.StatusInternalServerError)
					return
				}
				if !ok || pv.ID == "" {
					http.Error(w, "default project not found", http.StatusInternalServerError)
					return
				}
			}
			projCtx := auth.WithProject(r.Context(), tenant.Project{ID: pv.ID, Name: pv.Name})
			next.ServeHTTP(w, r.WithContext(projCtx))
			return
		}

		if apiKey == "" {
			http.Redirect(w, r, "/ui/login", http.StatusFound)
			return
		}

		p, ok, err := u.res.ResolveByAPIKey(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "auth lookup failed", http.StatusInternalServerError)
			return
		}
		if !ok || p.ID == "" {
			http.Redirect(w, r, "/ui/login?err=invalid_api_key", http.StatusFound)
			return
		}
		// Validate project_id and normalize name for display.
		pv, pvOK, err := u.ch.GetProjectByID(r.Context(), p.ID)
		if err != nil {
			http.Error(w, "project lookup failed", http.StatusInternalServerError)
			return
		}
		if !pvOK || pv.ID == "" {
			http.Redirect(w, r, "/ui/login?err=invalid_api_key", http.StatusFound)
			return
		}
		ctx := auth.WithProject(r.Context(), tenant.Project{ID: pv.ID, Name: pv.Name})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (u *UI) getUIProjectID(r *http.Request) string {
	c, err := r.Cookie(uiProjectIDCookie)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(c.Value)
}

func (u *UI) setUIProjectID(w http.ResponseWriter, projectID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     uiProjectIDCookie,
		Value:    strings.TrimSpace(projectID),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   60 * 60 * 24 * 30, // 30 days
	})
}

func (u *UI) clearUIProjectID(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     uiProjectIDCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

func safeNextUI(next string) string {
	next = strings.TrimSpace(next)
	if next == "" {
		return "/ui/issues"
	}
	// Only allow relative navigation within /ui to avoid open redirects.
	if strings.HasPrefix(next, "/ui/") || next == "/ui" {
		return next
	}
	return "/ui/issues"
}

func (u *UI) selectProject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	next := safeNextUI(r.FormValue("next"))
	projectID := strings.TrimSpace(r.FormValue("project_id"))
	if projectID == "" {
		u.clearUIProjectID(w)
		http.Redirect(w, r, next, http.StatusFound)
		return
	}
	pv, ok, err := u.ch.GetProjectByID(r.Context(), projectID)
	if err != nil {
		http.Error(w, "project lookup failed", http.StatusInternalServerError)
		return
	}
	if !ok || pv.ID == "" {
		http.Error(w, "unknown project_id", http.StatusBadRequest)
		return
	}
	u.setUIProjectID(w, pv.ID)
	http.Redirect(w, r, next, http.StatusFound)
}

func (u *UI) topbarProjects(ctx context.Context) []clickhouse.ProjectView {
	items, err := u.ch.ListProjects(ctx, 200)
	if err != nil {
		return nil
	}
	return items
}

type loginData struct {
	baseData
	UIAuthMode   string
	HasUISession bool
}

func (u *UI) login(w http.ResponseWriter, r *http.Request) {
	mode := strings.ToLower(strings.TrimSpace(u.cfg.UIAuthMode))
	claims, hasSess := u.getUISession(r)

	// Azure mode: GET /ui/login starts the OIDC flow unless already logged in.
	if mode == "azure" && r.Method == http.MethodGet && !hasSess {
		returnTo := r.URL.Query().Get("next")
		u.azureStart(w, r, returnTo)
		return
	}

	switch r.Method {
	case http.MethodGet:
		errMsg := ""
		switch r.URL.Query().Get("err") {
		case "invalid_api_key":
			errMsg = "Invalid API key."
		}
		_ = u.tpl["login.html"].ExecuteTemplate(w, "login.html", loginData{
			baseData: baseData{
				Title:      "Login",
				Project:    tenant.Project{}, // set by withProject on authed routes
				CurrentURI: r.URL.RequestURI(),
				Now:        time.Now().UTC(),
				Error:      errMsg,
			},
			UIAuthMode:   mode,
			HasUISession: hasSess && claims.Sub != "",
		})
		return
	case http.MethodPost:
		// In Azure mode, require an existing UI session before allowing project selection.
		if mode == "azure" && !hasSess {
			http.Redirect(w, r, "/ui/login", http.StatusFound)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		apiKey := strings.TrimSpace(r.FormValue("api_key"))
		if apiKey == "" {
			http.Redirect(w, r, "/ui/login?err=invalid_api_key", http.StatusFound)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     apiKeyCookie,
			Value:    apiKey,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			// Note: Secure depends on TLS termination; leave false for local/dev.
			MaxAge: 60 * 60 * 24 * 30, // 30 days
		})
		http.Redirect(w, r, "/ui/issues", http.StatusFound)
		return
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
}

func (u *UI) logout(w http.ResponseWriter, r *http.Request) {
	// Clear UI session (Azure) and project selection cookie.
	u.clearUISession(w, r)
	http.SetCookie(w, &http.Cookie{
		Name:     apiKeyCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	u.clearUIProjectID(w)
	u.clearAzureOAuthState(w, r)
	// If UI auth is disabled, logging out should not block the UI.
	if strings.EqualFold(strings.TrimSpace(u.cfg.UIAuthMode), "disabled") {
		http.Redirect(w, r, "/ui/issues", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/ui/login", http.StatusFound)
}

func (u *UI) issues(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.ProjectFromContext(r.Context())
	q := r.URL.Query()

	service := q.Get("service")
	env := q.Get("env")
	status := strings.ToLower(strings.TrimSpace(q.Get("status")))
	fromStr := q.Get("from")
	toStr := q.Get("to")
	limit := parseInt(q.Get("limit"), 50)

	from := parseTimeUI(fromStr)
	to := parseToExclusiveTimeUI(toStr)

	items, err := u.ch.ListIssues(r.Context(), p.ID, service, env, from, to, limit)
	if err != nil {
		http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if status != "" {
		filtered := make([]api.IssueSummary, 0, len(items))
		for _, it := range items {
			if strings.EqualFold(it.Status, status) {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	u.renderPageOrPartial(w, r, "issues.html", issuesData{
		baseData: baseData{
			Title:      "Issues",
			Project:    p,
			Projects:   u.topbarProjects(r.Context()),
			CurrentURI: r.URL.RequestURI(),
			Now:        time.Now().UTC(),
			PollSeconds: u.cfg.UIPollSeconds,
		},
		Service: service,
		Env:     env,
		Status:  status,
		From:    fromStr,
		To:      toStr,
		Limit:   limit,
		Issues:  items,
	})
}

func (u *UI) issueDetail(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.ProjectFromContext(r.Context())
	issueID := strings.TrimPrefix(r.URL.Path, "/ui/issues/")
	issueID = strings.TrimSpace(issueID)
	issueID = strings.TrimSuffix(issueID, "/")
	if issueID == "" {
		http.Redirect(w, r, "/ui/issues", http.StatusFound)
		return
	}

	qp := r.URL.Query()
	occLimit := parseInt(qp.Get("limit"), 50)
	fromStr := qp.Get("from")
	toStr := qp.Get("to")
	back := safeNextUI(qp.Get("back"))

	from := parseTimeUI(fromStr)
	to := parseToExclusiveTimeUI(toStr)

	summary, ok, err := u.ch.GetIssueSummary(r.Context(), p.ID, issueID)
	if err != nil {
		http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var issue *api.IssueSummary
	if ok {
		issue = &summary
	}

	occs, err := u.ch.ListIssueOccurrences(r.Context(), p.ID, issueID, from, to, occLimit)
	if err != nil {
		http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var latestFrames []frame
	if len(occs) > 0 {
		latestFrames = framesFromOccurrence(occs[0])
	}

	u.renderPageOrPartial(w, r, "issue_detail.html", issueDetailData{
		baseData: baseData{
			Title:      fmt.Sprintf("Issue %s", issueID),
			Project:    p,
			Projects:   u.topbarProjects(r.Context()),
			CurrentURI: r.URL.RequestURI(),
			Now:        time.Now().UTC(),
			PollSeconds: u.cfg.UIPollSeconds,
		},
		IssueID:      issueID,
		Issue:        issue,
		From:         fromStr,
		To:           toStr,
		Limit:        occLimit,
		Back:         back,
		Occurrences:  occs,
		LatestFrames: latestFrames,
	})
}

func (u *UI) logs(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.ProjectFromContext(r.Context())
	qp := r.URL.Query()

	service := qp.Get("service")
	env := qp.Get("env")
	level := strings.TrimSpace(qp.Get("level"))
	search := qp.Get("q")
	traceID := qp.Get("trace_id")
	requestID := qp.Get("request_id")
	limit := parseInt(qp.Get("limit"), 200)

	fromStr := qp.Get("from")
	toStr := qp.Get("to")
	from := parseTimeUI(fromStr)
	to := parseTimeUI(toStr)

	// Cursor pagination:
	// - URL may contain a stack of `cursor` query params; each cursor is a timestamp boundary (exclusive upper bound).
	// - The "current page" uses the *last* cursor as its effective `to`.
	// - "Older" appends the oldest timestamp from the current page.
	// - "Newer" pops one cursor from the stack.
	cursors := make([]string, 0, len(qp["cursor"]))
	for _, c := range qp["cursor"] {
		c = strings.TrimSpace(c)
		if c != "" {
			cursors = append(cursors, c)
		}
	}
	if len(cursors) > 0 {
		if t := parseTimeUI(cursors[len(cursors)-1]); !t.IsZero() {
			to = t
		}
	}

	fetchLimit := limit
	if fetchLimit < 500 {
		fetchLimit = limit + 1
	}

	events, err := u.ch.ListLogs(r.Context(), p.ID, service, env, level, search, traceID, requestID, from, to, fetchLimit)
	if err != nil {
		http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	hasOlder := false
	if len(events) > limit {
		hasOlder = true
		events = events[:limit]
	} else if fetchLimit == limit && limit >= 500 && len(events) == limit {
		// Best-effort when limit is maxed and we can't ask for limit+1.
		hasOlder = true
	}
	hasNewer := len(cursors) > 0

	cloneWithCursors := func(newCursors []string) string {
		q := r.URL.Query()
		q.Del("cursor")
		for _, c := range newCursors {
			q.Add("cursor", c)
		}
		u2 := *r.URL
		u2.RawQuery = q.Encode()
		return u2.String()
	}

	newerURL := ""
	if hasNewer {
		newerURL = cloneWithCursors(cursors[:len(cursors)-1])
	}
	olderURL := ""
	if len(events) > 0 {
		// Oldest event is the last row (ORDER BY ts DESC).
		oldest := events[len(events)-1].TS.UTC()
		olderCursors := append(append([]string{}, cursors...), strconv.FormatInt(oldest.UnixMilli(), 10))
		olderURL = cloneWithCursors(olderCursors)
	}

	u.renderPageOrPartial(w, r, "logs.html", logsData{
		baseData: baseData{
			Title:      "Logs",
			Project:    p,
			Projects:   u.topbarProjects(r.Context()),
			CurrentURI: r.URL.RequestURI(),
			Now:        time.Now().UTC(),
			PollSeconds: u.cfg.UIPollSeconds,
		},
		Service:   service,
		Env:       env,
		Level:     level,
		Q:         search,
		TraceID:   traceID,
		RequestID: requestID,
		From:      fromStr,
		To:        toStr,
		Limit:     limit,
		Cursors:   cursors,
		NewerURL:  newerURL,
		OlderURL:  olderURL,
		HasNewer:  hasNewer,
		HasOlder:  hasOlder,
		Events:    events,
	})
}

func (u *UI) dashboards(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.ProjectFromContext(r.Context())
	qp := r.URL.Query()

	service := qp.Get("service")
	env := qp.Get("env")
	fromStr := qp.Get("from")
	toStr := qp.Get("to")
	bucket := parseInt(qp.Get("bucket_seconds"), 60)

	from := parseTimeUI(fromStr)
	to := parseTimeUI(toStr)

	pts, err := u.ch.ErrorTrend(r.Context(), p.ID, service, env, "", "", from, to, bucket)
	if err != nil {
		http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	svg := renderTrendSVG(pts, 820, 180)

	u.renderPageOrPartial(w, r, "dashboards.html", dashboardsData{
		baseData: baseData{
			Title:      "Dashboards",
			Project:    p,
			Projects:   u.topbarProjects(r.Context()),
			CurrentURI: r.URL.RequestURI(),
			Now:        time.Now().UTC(),
			PollSeconds: u.cfg.UIPollSeconds,
		},
		Service: service,
		Env:     env,
		From:    fromStr,
		To:      toStr,
		Bucket:  bucket,
		Trend:   pts,
		SVG:     svg,
	})
}

func parseInt(s string, def int) int {
	if strings.TrimSpace(s) == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func parseTimeUI(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	// datetime-local (e.g. 2025-12-13T10:15 or 2025-12-13T10:15:30)
	if t, err := time.Parse("2006-01-02T15:04", s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return t.UTC()
	}
	// unix seconds/millis
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 && n < 1_000_000_000_000 {
		if n > 10_000_000_000 {
			return time.UnixMilli(n).UTC()
		}
		return time.Unix(n, 0).UTC()
	}
	// RFC3339 / RFC3339Nano
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// parseToExclusiveTimeUI parses the UI time and normalizes it for use as an exclusive upper bound.
// For datetime-local inputs without seconds (minute precision), users typically expect "to" to include
// the full minute; we convert it to [to, to+1m) for queries that use "< to".
func parseToExclusiveTimeUI(s string) time.Time {
	t := parseTimeUI(s)
	if t.IsZero() {
		return t
	}
	raw := strings.TrimSpace(s)
	// datetime-local minute precision: "2006-01-02T15:04" (len=16, exactly one colon)
	if len(raw) == len("2006-01-02T15:04") && strings.Count(raw, ":") == 1 {
		return t.Add(time.Minute)
	}
	return t
}

func framesFromOccurrence(occ api.ErrorOccurrence) []frame {
	out := make([]frame, 0, len(occ.StackFrames))
	for _, m := range occ.StackFrames {
		f := frame{}
		if v, ok := m["filename"].(string); ok {
			f.Filename = v
		}
		if v, ok := m["function"].(string); ok {
			f.Function = v
		}
		switch v := m["lineno"].(type) {
		case uint32:
			f.Lineno = v
		case uint64:
			f.Lineno = uint32(v)
		case int:
			f.Lineno = uint32(v)
		case int64:
			f.Lineno = uint32(v)
		case float64:
			f.Lineno = uint32(v)
		}
		if v, ok := m["context"].(string); ok {
			f.Context = v
		}
		switch v := m["is_in_app"].(type) {
		case uint8:
			f.IsInApp = v == 1
		case uint64:
			f.IsInApp = v == 1
		case int:
			f.IsInApp = v == 1
		case bool:
			f.IsInApp = v
		}
		out = append(out, f)
	}
	return out
}

func renderTrendSVG(points []api.TimeSeriesPoint, width, height int) template.HTML {
	// Minimal sparkline-style SVG.
	if width <= 0 {
		width = 800
	}
	if height <= 0 {
		height = 180
	}
	if len(points) == 0 {
		return template.HTML(fmt.Sprintf(`<svg class="chart" width="%d" height="%d" viewBox="0 0 %d %d"><text x="12" y="24" class="muted">No data</text></svg>`, width, height, width, height))
	}

	var max uint64 = 1
	for _, p := range points {
		if p.Count > max {
			max = p.Count
		}
	}
	n := len(points)
	xStep := float64(width-20) / float64(maxInt(1, n-1))
	yMax := float64(max)

	path := strings.Builder{}
	for i, p := range points {
		x := 10.0 + float64(i)*xStep
		y := float64(height-20) - (float64(p.Count)/yMax)*float64(height-40)
		if i == 0 {
			path.WriteString(fmt.Sprintf("M%.2f %.2f", x, y))
		} else {
			path.WriteString(fmt.Sprintf(" L%.2f %.2f", x, y))
		}
	}

	s := fmt.Sprintf(
		`<svg class="chart" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-label="Error trend">
  <rect x="0" y="0" width="%d" height="%d" rx="10" class="chart-bg"></rect>
  <path d="%s" fill="none" class="chart-line"></path>
  <text x="14" y="%d" class="chart-label">max=%d</text>
</svg>`,
		width, height, width, height,
		width, height,
		path.String(),
		height-10, max,
	)
	return template.HTML(s)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
