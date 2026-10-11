// Package webui serves the browser UI: the embedded React application under /ui/ and the JSON
// API it talks to under /ui/api/ (mirrored under /v1/q/ for API-key clients).
package webui

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"pyloggle/internal/config"
	"pyloggle/internal/issue"
	"pyloggle/internal/storage/clickhouse"
	"pyloggle/internal/sysinfo"
	"pyloggle/internal/tenant"
)

const apiKeyCookie = "pyloggle_api_key"
const uiProjectIDCookie = "pyloggle_ui_project_id"

type UI struct {
	cfg  config.Config
	ch   *clickhouse.Client
	res  *tenant.Resolver
	jwks *jwksCache

	// JSON API dependencies (fakeable in tests).
	store  Store
	keys   KeyResolver
	states *issue.StateCache // shared with ingest so UI state changes are seen at once
	api    http.Handler
	spa    http.Handler

	sys       *sysinfo.Collector // System page: ClickHouse snapshot (nil in tests)
	startedAt time.Time
}

// WithSystem attaches the ClickHouse collector and the process start time for the System page.
func (u *UI) WithSystem(c *sysinfo.Collector, startedAt time.Time) *UI {
	u.sys, u.startedAt = c, startedAt
	return u
}

// New builds the UI handler. states may be nil (then ingest learns about UI state changes after
// its cache TTL).
func New(cfg config.Config, ch *clickhouse.Client, res *tenant.Resolver, states *issue.StateCache) *UI {
	u := &UI{cfg: cfg, ch: ch, res: res, states: states}
	if ch != nil {
		u.store = ch
	}
	if res != nil {
		u.keys = res
	}
	u.api = u.uiAPIHandler()
	u.spa = newSPAHandler()
	return u
}

// NewForTest builds a UI around fakes (no ClickHouse).
func NewForTest(cfg config.Config, store Store, keys KeyResolver, states *issue.StateCache) *UI {
	u := &UI{cfg: cfg, store: store, keys: keys, states: states}
	u.api = u.uiAPIHandler()
	u.spa = newSPAHandler()
	return u
}

// loginRequired reports whether the UI is behind an OIDC login (azure preset or generic).
func (u *UI) loginRequired() bool { return u.cfg.OIDC().Enabled() }

func (u *UI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/ui" {
		http.Redirect(w, r, "/ui/", http.StatusMovedPermanently)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/ui/") {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/ui/api/") {
		u.api.ServeHTTP(w, r)
		return
	}
	switch r.URL.Path {
	case "/ui/login":
		u.login(w, r)
		return
	case "/ui/oauth2/callback":
		u.oidcCallback(w, r)
		return
	case "/ui/logout":
		u.logout(w, r)
		return
	}
	// Everything else is the single-page app. Static assets are public; behind a login the app
	// shell itself is only served to a signed-in browser, so an anonymous visit bounces straight
	// into the OIDC flow instead of showing an empty shell.
	if u.loginRequired() && !isAssetPath(r.URL.Path) {
		if _, ok := u.getUISession(r); !ok {
			http.Redirect(w, r, "/ui/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
	}
	u.spa.ServeHTTP(w, r)
}

// login starts the OIDC flow when a login is required and no session exists; otherwise the
// SPA renders its own login screen (API-key exchange happens through POST /ui/api/login).
func (u *UI) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if u.loginRequired() {
		if _, ok := u.getUISession(r); !ok {
			u.oidcStart(w, r, r.URL.Query().Get("next"))
			return
		}
		http.Redirect(w, r, safeNextUI(r.URL.Query().Get("next")), http.StatusFound)
		return
	}
	u.spa.ServeHTTP(w, r)
}

func (u *UI) logout(w http.ResponseWriter, r *http.Request) {
	u.clearUISession(w, r)
	http.SetCookie(w, &http.Cookie{Name: apiKeyCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	u.clearUIProjectID(w)
	u.clearOAuthState(w, r)
	if !u.loginRequired() {
		http.Redirect(w, r, "/ui/issues", http.StatusFound)
		return
	}
	http.Redirect(w, r, "/ui/login", http.StatusFound)
}

func safeNextUI(next string) string {
	next = strings.TrimSpace(next)
	if strings.HasPrefix(next, "/ui/") || next == "/ui" {
		return next
	}
	return "/ui/issues"
}

func (u *UI) setActiveProjectCookie(w http.ResponseWriter, apiKey string) {
	http.SetCookie(w, &http.Cookie{
		Name:     apiKeyCookie,
		Value:    strings.TrimSpace(apiKey),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Secure depends on TLS termination; left off for local/dev.
		MaxAge: 60 * 60 * 24 * 30,
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
		MaxAge:   60 * 60 * 24 * 30,
	})
}

func (u *UI) clearUIProjectID(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: uiProjectIDCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
}
