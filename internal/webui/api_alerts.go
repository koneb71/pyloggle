package webui

import (
	"net/http"
	"strings"

	"pyloggle/internal/api"
	"pyloggle/internal/auth"
)

var alertConditions = map[string]bool{"error_rate": true, "issue_spike": true, "new_issue": true, "regression": true}

func (u *UI) apiAlertRules(w http.ResponseWriter, r *http.Request) {
	rules, err := u.store.ListAlertRules(r.Context(), auth.ProjectIDFromContext(r.Context()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if rules == nil {
		rules = []api.AlertRule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rules, "conditions": []string{"error_rate", "issue_spike", "new_issue", "regression"}})
}

func (u *UI) apiAlertRuleUpsert(w http.ResponseWriter, r *http.Request) {
	if !u.requireAdmin(w, r) {
		return
	}
	var body struct {
		Rule api.AlertRule `json:"rule"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "bad_json", err.Error())
		return
	}
	rule := body.Rule
	rule.ProjectID = auth.ProjectIDFromContext(r.Context())
	rule.ConditionType = strings.ToLower(strings.TrimSpace(rule.ConditionType))
	if !alertConditions[rule.ConditionType] {
		writeErr(w, http.StatusBadRequest, "bad_condition", "condition_type must be one of error_rate, issue_spike, new_issue, regression")
		return
	}
	if strings.TrimSpace(rule.Name) == "" {
		writeErr(w, http.StatusBadRequest, "missing_name", "name is required")
		return
	}
	saved, err := u.store.UpsertAlertRule(r.Context(), rule)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rule": saved})
}

func (u *UI) apiAlertFirings(w http.ResponseWriter, r *http.Request) {
	items, err := u.store.ListAlertFirings(r.Context(), auth.ProjectIDFromContext(r.Context()), qInt(r, "limit", 50, 500))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if items == nil {
		items = []api.AlertFiring{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
