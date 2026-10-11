package webui

import (
	"net/http"
	"strings"

	"pyloggle/internal/auth"
)

type meResponse struct {
	AuthMode string      `json:"auth_mode"`
	User     *meUser     `json:"user,omitempty"`
	IsAdmin  bool        `json:"is_admin"`
	Project  *meProject  `json:"project,omitempty"`
	Projects []meProject `json:"projects"`
	Features meFeatures  `json:"features"`
}

type meUser struct {
	Sub   string `json:"sub"`
	Name  string `json:"name,omitempty"`
	Email string `json:"email,omitempty"`
}

type meProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type meFeatures struct {
	Alerting  bool   `json:"alerting"`
	Traces    bool   `json:"traces"`
	PublicURL string `json:"public_url,omitempty"`
}

func (u *UI) apiMe(w http.ResponseWriter, r *http.Request) {
	resp := meResponse{AuthMode: strings.ToLower(strings.TrimSpace(u.cfg.UIAuthMode)), IsAdmin: u.isUIAdmin(r),
		Projects: []meProject{}, Features: meFeatures{Alerting: u.cfg.AlertingEnabled, Traces: u.cfg.OTLPStoreSpans, PublicURL: u.cfg.PublicURL}}
	if c, ok := uiUserFromContext(r.Context()); ok {
		resp.User = &meUser{Sub: c.Sub, Name: c.Name, Email: c.Email}
		if resp.User.Email == "" {
			resp.User.Email = c.PreferredUsername
		}
	}
	if p, ok := auth.ProjectFromContext(r.Context()); ok && p.ID != "" {
		resp.Project = &meProject{ID: p.ID, Name: p.Name}
	}
	if items, err := u.store.ListProjects(r.Context(), 200); err == nil {
		for _, it := range items {
			resp.Projects = append(resp.Projects, meProject{ID: it.ID, Name: it.Name})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (u *UI) apiLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIKey string `json:"api_key"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	key := strings.TrimSpace(body.APIKey)
	if key == "" || u.keys == nil {
		writeErr(w, http.StatusBadRequest, "invalid_api_key", "api_key is required")
		return
	}
	p, ok, err := u.keys.ResolveByAPIKey(r.Context(), key)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "lookup_failed", err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusUnauthorized, "invalid_api_key", "unknown API key")
		return
	}
	u.setActiveProjectCookie(w, key)
	u.setUIProjectID(w, p.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "project": meProject{ID: p.ID, Name: p.Name}})
}

func (u *UI) apiLogout(w http.ResponseWriter, r *http.Request) {
	u.clearUISession(w, r)
	http.SetCookie(w, &http.Cookie{Name: apiKeyCookie, Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	u.clearUIProjectID(w)
	u.clearAzureOAuthState(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (u *UI) apiSelectProject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ProjectID string `json:"project_id"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	id := strings.TrimSpace(body.ProjectID)
	if id == "" {
		u.clearUIProjectID(w)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	pv, ok, err := u.store.GetProjectByID(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "lookup_failed", err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusBadRequest, "unknown_project", "unknown project_id")
		return
	}
	u.setUIProjectID(w, pv.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "project": meProject{ID: pv.ID, Name: pv.Name}})
}
