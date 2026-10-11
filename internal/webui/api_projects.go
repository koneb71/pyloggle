package webui

import (
	"net/http"
	"strings"

	"pyloggle/internal/api"
	"pyloggle/internal/storage/clickhouse"
)

func projectToAPI(p clickhouse.ProjectView) api.Project {
	return api.Project{ID: p.ID, Name: p.Name, Description: p.Description, MaxEventsPerSec: p.MaxEventsPerSec,
		MaxStorageGB: p.MaxStorageGB, APIKeyHash: p.APIKeyHash, APIKeyCreatedAt: p.APIKeyCreatedAt, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}

func (u *UI) apiProjects(w http.ResponseWriter, r *http.Request) {
	items, err := u.store.ListProjects(r.Context(), qInt(r, "limit", 200, 500))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	out := make([]api.Project, 0, len(items))
	for _, it := range items {
		out = append(out, projectToAPI(it))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (u *UI) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if u.isUIAdmin(r) {
		return true
	}
	writeErr(w, http.StatusForbidden, "admin_required", "this action is limited to PYLOGGLE_UI_ADMINS")
	return false
}

func (u *UI) apiProjectCreate(w http.ResponseWriter, r *http.Request) {
	if !u.requireAdmin(w, r) {
		return
	}
	var body api.ProjectCreateRequest
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	body.ID = strings.TrimSpace(body.ID)
	if body.ID == "" {
		writeErr(w, http.StatusBadRequest, "missing_id", "id is required")
		return
	}
	if _, exists, _ := u.store.GetProjectByID(r.Context(), body.ID); exists {
		writeErr(w, http.StatusConflict, "exists", "a project with this id already exists")
		return
	}
	pv, key, err := u.store.CreateProject(r.Context(), clickhouse.ProjectUpsert{ID: body.ID, Name: body.Name, Description: body.Description, MaxEventsPerSec: body.MaxEventsPerSec, MaxStorageGB: body.MaxStorageGB})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, api.ProjectCreateResponse{Project: projectToAPI(pv), APIKey: key})
}

func (u *UI) apiProjectUpdate(w http.ResponseWriter, r *http.Request) {
	if !u.requireAdmin(w, r) {
		return
	}
	var body api.ProjectUpdateRequest
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	id := r.PathValue("id")
	pv, err := u.store.UpsertProject(r.Context(), clickhouse.ProjectUpsert{ID: id, Name: body.Name, Description: body.Description, MaxEventsPerSec: body.MaxEventsPerSec, MaxStorageGB: body.MaxStorageGB})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": projectToAPI(pv)})
}

func (u *UI) apiProjectRotateKey(w http.ResponseWriter, r *http.Request) {
	if !u.requireAdmin(w, r) {
		return
	}
	pv, key, err := u.store.RotateProjectAPIKey(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "rotate_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, api.ProjectRotateKeyResponse{Project: projectToAPI(pv), APIKey: key})
}
