package webui

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"pyloggle/internal/api"
	"pyloggle/internal/storage/clickhouse"
)

func projectToAPI(p clickhouse.ProjectView) api.Project {
	return api.Project{ID: p.ID, Name: p.Name, Description: p.Description, MaxEventsPerSec: p.MaxEventsPerSec,
		MaxStorageGB: p.MaxStorageGB, RetentionDays: p.RetentionDays, APIKeyHash: p.APIKeyHash, APIKeyCreatedAt: p.APIKeyCreatedAt, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}

// maxRetentionDays is the longest per-project override: the server's own retention (an
// override only ever shortens).
func (u *UI) maxRetentionDays() int {
	n := u.cfg.RetentionLogsDays
	if u.cfg.RetentionErrorsDays > n {
		n = u.cfg.RetentionErrorsDays
	}
	if n <= 0 {
		n = 30
	}
	return n
}

func (u *UI) checkRetention(w http.ResponseWriter, days uint16) bool {
	if int(days) > u.maxRetentionDays() {
		writeErr(w, http.StatusBadRequest, "bad_retention", fmt.Sprintf("retention_days must be 0 (server default) or at most %d", u.maxRetentionDays()))
		return false
	}
	return true
}

func (u *UI) audit(r *http.Request, action, projectID, target, details string) {
	_ = u.store.AddAudit(r.Context(), api.AuditEntry{TS: time.Now().UTC(), Actor: currentUser(r), Action: action, ProjectID: projectID, Target: target, Details: details})
}

// apiProject returns one project; a deleted one answers while its purge is still running.
func (u *UI) apiProject(w http.ResponseWriter, r *http.Request) {
	pv, ok, err := u.store.GetProjectIncludingDeleted(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "unknown project")
		return
	}
	out := projectToAPI(pv)
	if pv.IsDeleted {
		n, _ := u.store.PendingMutations(r.Context(), pv.ID)
		if n == 0 {
			writeErr(w, http.StatusNotFound, "not_found", "project deleted")
			return
		}
		out.Deleting = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": out})
}

// apiProjectUsage reports the estimated storage of a project against its quota.
func (u *UI) apiProjectUsage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pv, ok, err := u.store.GetProjectByID(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "not_found", "unknown project")
		return
	}
	days := qInt(r, "days", 30, 400)
	daily, err := u.store.ProjectUsage(r.Context(), id, days)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	comp, _ := u.store.TableCompression(r.Context())
	if comp == nil {
		comp = map[string]float64{}
	}
	window := u.maxRetentionDays()
	cutoff := time.Now().UTC().AddDate(0, 0, -window)
	out := api.ProjectUsage{ProjectID: id, QuotaGB: pv.MaxStorageGB, WindowDays: window, BySource: map[string]api.UsageSource{}, Daily: daily, Compression: comp,
		Retention: api.UsageRetention{Logs: u.cfg.RetentionLogsDays, Errors: u.cfg.RetentionErrorsDays, Spans: u.cfg.RetentionSpansDays, Metrics: u.cfg.RetentionMetricsDays, OverrideDays: int(pv.RetentionDays)}}
	if daily == nil {
		out.Daily = []api.UsageDay{}
	}
	for _, d := range daily {
		if d.Day.Before(cutoff) {
			continue
		}
		src := out.BySource[d.Source]
		src.Rows += d.Rows
		src.BytesEst += d.BytesEst
		src.DiskEst += uint64(float64(d.BytesEst) * clickhouse.UsageRatio(comp, d.Source))
		out.BySource[d.Source] = src
		out.StoredBytesEst += d.BytesEst
	}
	for _, src := range out.BySource {
		out.StoredDiskEst += src.DiskEst
	}
	if pv.MaxStorageGB > 0 {
		out.StoredPct = float64(out.StoredDiskEst) / float64(uint64(pv.MaxStorageGB)<<30) * 100
	}
	if n, err := u.store.PendingMutations(r.Context(), id); err == nil && n > 0 {
		out.Deleting = true
	}
	writeJSON(w, http.StatusOK, out)
}

// apiProjectDelete tombstones the project, drops its keys from the resolver and purges the
// rows in the background. 202 because the mutations take a while on large tables.
func (u *UI) apiProjectDelete(w http.ResponseWriter, r *http.Request) {
	if !u.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if _, ok, err := u.store.GetProjectByID(r.Context(), id); err != nil || !ok {
		writeErr(w, http.StatusNotFound, "not_found", "unknown project")
		return
	}
	if err := u.store.MarkProjectDeleted(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "delete_failed", err.Error())
		return
	}
	if inv, ok := u.keys.(interface{ InvalidateProject(string) }); ok && inv != nil {
		inv.InvalidateProject(id)
	}
	u.audit(r, "project.delete", id, "", "")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		if err := u.store.PurgeProjectData(ctx, id); err != nil {
			log.Printf("project %s purge: %v", id, err)
		}
		_ = u.store.RecomputeUsage(ctx, id)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "deleting", "id": id})
}

// apiProjectPurgeUser removes one user's log lines and error occurrences.
func (u *UI) apiProjectPurgeUser(w http.ResponseWriter, r *http.Request) {
	if !u.requireAdmin(w, r) {
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	var body struct {
		UserIDHash string `json:"user_id_hash"`
		UserEmail  string `json:"user_email"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	body.UserIDHash, body.UserEmail = strings.TrimSpace(body.UserIDHash), strings.TrimSpace(body.UserEmail)
	if body.UserIDHash == "" && body.UserEmail == "" {
		writeErr(w, http.StatusBadRequest, "missing_user", "user_id_hash or user_email is required")
		return
	}
	if _, ok, err := u.store.GetProjectByID(r.Context(), id); err != nil || !ok {
		writeErr(w, http.StatusNotFound, "not_found", "unknown project")
		return
	}
	if err := u.store.PurgeUser(r.Context(), id, body.UserIDHash, body.UserEmail); err != nil {
		writeErr(w, http.StatusInternalServerError, "purge_failed", err.Error())
		return
	}
	target := body.UserIDHash
	if target == "" {
		target = body.UserEmail
	}
	u.audit(r, "user.purge", id, target, "")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		_ = u.store.RecomputeUsage(ctx, id)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "purging", "id": id})
}

func (u *UI) apiAudit(w http.ResponseWriter, r *http.Request) {
	if !u.requireAdmin(w, r) {
		return
	}
	items, err := u.store.ListAudit(r.Context(), strings.TrimSpace(r.URL.Query().Get("project_id")), qInt(r, "limit", 100, 1000))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if items == nil {
		items = []api.AuditEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
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
	if _, exists, _ := u.store.GetProjectIncludingDeleted(r.Context(), body.ID); exists {
		writeErr(w, http.StatusConflict, "exists", "a project with this id already exists (or was deleted; ids are not reused)")
		return
	}
	if !u.checkRetention(w, body.RetentionDays) {
		return
	}
	pv, key, err := u.store.CreateProject(r.Context(), clickhouse.ProjectUpsert{ID: body.ID, Name: body.Name, Description: body.Description, MaxEventsPerSec: body.MaxEventsPerSec, MaxStorageGB: body.MaxStorageGB, RetentionDays: body.RetentionDays})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	u.audit(r, "project.create", pv.ID, "", "")
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
	if !u.checkRetention(w, body.RetentionDays) {
		return
	}
	pv, err := u.store.UpsertProject(r.Context(), clickhouse.ProjectUpsert{ID: id, Name: body.Name, Description: body.Description, MaxEventsPerSec: body.MaxEventsPerSec, MaxStorageGB: body.MaxStorageGB, RetentionDays: body.RetentionDays})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}
	if inv, ok := u.keys.(interface{ InvalidateProject(string) }); ok && inv != nil {
		inv.InvalidateProject(id)
	}
	u.audit(r, "project.update", id, "", fmt.Sprintf("quota_gb=%d retention_days=%d", body.MaxStorageGB, body.RetentionDays))
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
	if inv, ok := u.keys.(interface{ InvalidateProject(string) }); ok && inv != nil {
		inv.InvalidateProject(pv.ID)
	}
	u.audit(r, "key.rotate", pv.ID, "", "")
	writeJSON(w, http.StatusOK, api.ProjectRotateKeyResponse{Project: projectToAPI(pv), APIKey: key})
}
