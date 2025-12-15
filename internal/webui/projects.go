package webui

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"pyloggle/internal/auth"
	"pyloggle/internal/storage/clickhouse"
	"pyloggle/internal/tenant"
)

const (
	uiFlashAPIKeyCookie = "pyloggle_ui_flash_api_key"
)

type projectsFlash struct {
	ProjectID string `json:"project_id"`
	APIKey    string `json:"api_key"`
	Action    string `json:"action"` // created|rotated
}

func (u *UI) setFlashAPIKey(w http.ResponseWriter, r *http.Request, projectID, apiKey, action string) {
	// Store in an HttpOnly cookie so it can be rendered server-side exactly once, then cleared.
	b, _ := json.Marshal(projectsFlash{
		ProjectID: strings.TrimSpace(projectID),
		APIKey:    strings.TrimSpace(apiKey),
		Action:    strings.TrimSpace(action),
	})
	val := base64.RawURLEncoding.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{
		Name:     uiFlashAPIKeyCookie,
		Value:    val,
		Path:     "/ui/projects",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Note: Secure depends on TLS termination; leave false for local/dev.
		MaxAge: 120,
	})
}

func (u *UI) clearFlashAPIKey(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     uiFlashAPIKeyCookie,
		Value:    "",
		Path:     "/ui/projects",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

func (u *UI) getFlashAPIKey(r *http.Request) (projectsFlash, bool) {
	c, err := r.Cookie(uiFlashAPIKeyCookie)
	if err != nil || strings.TrimSpace(c.Value) == "" {
		return projectsFlash{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(c.Value))
	if err != nil {
		return projectsFlash{}, false
	}
	var f projectsFlash
	if err := json.Unmarshal(raw, &f); err != nil {
		return projectsFlash{}, false
	}
	if strings.TrimSpace(f.APIKey) == "" || strings.TrimSpace(f.ProjectID) == "" {
		return projectsFlash{}, false
	}
	return f, true
}

func (u *UI) maybeResolveActiveProject(r *http.Request) tenant.Project {
	// Prefer explicit UI project selection (project_id cookie).
	if pid := strings.TrimSpace(u.getUIProjectID(r)); pid != "" {
		if pv, ok, err := u.ch.GetProjectByID(r.Context(), pid); err == nil && ok {
			return tenant.Project{ID: pv.ID, Name: pv.Name}
		}
	}

	apiKey := ""
	if c, err := r.Cookie(apiKeyCookie); err == nil {
		apiKey = strings.TrimSpace(c.Value)
	}
	if apiKey == "" {
		// Single-tenant fallback for displaying the current project in the topbar.
		if strings.TrimSpace(u.cfg.DefaultProjID) != "" {
			if pv, ok, err := u.ch.GetProjectByID(r.Context(), strings.TrimSpace(u.cfg.DefaultProjID)); err == nil && ok {
				return tenant.Project{ID: pv.ID, Name: pv.Name}
			}
		}
		return tenant.Project{}
	}
	p, ok, err := u.res.ResolveByAPIKey(r.Context(), apiKey)
	if err != nil || !ok {
		return tenant.Project{}
	}
	return p
}

func (u *UI) setActiveProjectCookie(w http.ResponseWriter, apiKey string) {
	http.SetCookie(w, &http.Cookie{
		Name:     apiKeyCookie,
		Value:    strings.TrimSpace(apiKey),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		// Note: Secure depends on TLS termination; leave false for local/dev.
		MaxAge: 60 * 60 * 24 * 30, // 30 days
	})
}

type projectsData struct {
	baseData
	Projects []clickhouse.ProjectView

	Flash *projectsFlash
}

func (u *UI) projectsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limit := parseInt(r.URL.Query().Get("limit"), 200)
	items, err := u.ch.ListProjects(r.Context(), limit)
	if err != nil {
		http.Error(w, "query failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var flash *projectsFlash
	if f, ok := u.getFlashAPIKey(r); ok {
		flash = &f
		// Clear immediately so refresh/back doesn't re-show it.
		u.clearFlashAPIKey(w)
	}

	_ = u.tpl["projects.html"].ExecuteTemplate(w, "projects.html", projectsData{
		baseData: baseData{
			Title:   "Projects",
			Project: u.maybeResolveActiveProject(r), // for the topbar display only
			Projects: items,
			CurrentURI: r.URL.RequestURI(),
			Now:     time.Now().UTC(),
		},
		Projects: items,
		Flash:    flash,
	})
}

func (u *UI) projectsCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	name := strings.TrimSpace(r.FormValue("name"))
	desc := strings.TrimSpace(r.FormValue("description"))
	maxEPS := uint32(parseInt(r.FormValue("max_events_per_second"), 0))
	maxGB := uint32(parseInt(r.FormValue("max_storage_gb"), 0))

	p, apiKey, err := u.ch.CreateProject(r.Context(), clickhouse.ProjectUpsert{
		ID:              id,
		Name:            name,
		Description:     desc,
		MaxEventsPerSec: maxEPS,
		MaxStorageGB:    maxGB,
	})
	if err != nil {
		http.Error(w, "create failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	u.setFlashAPIKey(w, r, p.ID, apiKey, "created")
	if strings.TrimSpace(r.FormValue("set_active")) != "" {
		u.setActiveProjectCookie(w, apiKey)
	}
	http.Redirect(w, r, "/ui/projects", http.StatusFound)
}

func (u *UI) projectsUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	name := strings.TrimSpace(r.FormValue("name"))
	desc := strings.TrimSpace(r.FormValue("description"))
	maxEPS := uint32(parseInt(r.FormValue("max_events_per_second"), 0))
	maxGB := uint32(parseInt(r.FormValue("max_storage_gb"), 0))

	if _, err := u.ch.UpsertProject(r.Context(), clickhouse.ProjectUpsert{
		ID:              id,
		Name:            name,
		Description:     desc,
		MaxEventsPerSec: maxEPS,
		MaxStorageGB:    maxGB,
	}); err != nil {
		http.Error(w, "update failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/ui/projects", http.StatusFound)
}

func (u *UI) projectsRotateKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(r.FormValue("id"))
	p, apiKey, err := u.ch.RotateProjectAPIKey(r.Context(), id)
	if err != nil {
		http.Error(w, "rotate failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	u.setFlashAPIKey(w, r, p.ID, apiKey, "rotated")
	if strings.TrimSpace(r.FormValue("set_active")) != "" {
		u.setActiveProjectCookie(w, apiKey)
	}
	http.Redirect(w, r, "/ui/projects", http.StatusFound)
}

// Ensure this file imports auth so it isn't accidentally pruned in future when we add user context.
// (Today, Projects UI is "any signed-in user"; project auth is separate.)
var _ = auth.ProjectIDFromContext


