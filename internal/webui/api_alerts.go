package webui

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"pyloggle/internal/alerting"
	"pyloggle/internal/api"
	"pyloggle/internal/auth"
	"pyloggle/internal/logquery"
	"pyloggle/internal/storage/clickhouse"
)

var alertConditions = map[string]bool{"error_rate": true, "issue_spike": true, "new_issue": true, "regression": true, "log_query": true, "daily_recap": true}
var alertComparisons = map[string]bool{"gt": true, "gte": true, "lt": true, "lte": true, "eq": true}
var alertConditionList = []string{"error_rate", "issue_spike", "new_issue", "regression", "log_query", "daily_recap"}

func (u *UI) apiAlertRules(w http.ResponseWriter, r *http.Request) {
	rules, err := u.store.ListAlertRules(r.Context(), auth.ProjectIDFromContext(r.Context()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	if rules == nil {
		rules = []api.AlertRule{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rules, "conditions": alertConditionList})
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
		writeErr(w, http.StatusBadRequest, "bad_condition", "condition_type must be one of "+strings.Join(alertConditionList, ", "))
		return
	}
	if strings.TrimSpace(rule.Name) == "" {
		writeErr(w, http.StatusBadRequest, "missing_name", "name is required")
		return
	}
	rule.Query = strings.TrimSpace(rule.Query)
	rule.Comparison = strings.ToLower(strings.TrimSpace(rule.Comparison))
	if rule.ConditionType == "log_query" {
		if rule.Query == "" {
			writeErr(w, http.StatusBadRequest, "missing_query", "log_query rules need a query")
			return
		}
		if _, err := logquery.Parse(rule.Query); err != nil {
			writeQueryErr(w, err)
			return
		}
		if rule.Comparison == "" {
			rule.Comparison = "gt"
		}
		if !alertComparisons[rule.Comparison] {
			writeErr(w, http.StatusBadRequest, "bad_comparison", "comparison must be one of gt, gte, lt, lte, eq")
			return
		}
		if rule.Threshold < 0 {
			writeErr(w, http.StatusBadRequest, "bad_threshold", "threshold must be >= 0")
			return
		}
		if rule.TimeWindowSeconds != 0 && rule.TimeWindowSeconds < 60 {
			writeErr(w, http.StatusBadRequest, "bad_window", "time_window_seconds must be at least 60")
			return
		}
	}
	if rule.ConditionType == "daily_recap" {
		if code, msg := normalizeRecapRule(&rule); code != "" {
			writeErr(w, http.StatusBadRequest, code, msg)
			return
		}
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

// apiAlertRulePreview counts what a log_query rule would see right now ("Test" in the form).
func (u *UI) apiAlertRulePreview(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	raw := strings.TrimSpace(qs.Get("query"))
	if raw == "" {
		writeErr(w, http.StatusBadRequest, "missing_query", "query is required")
		return
	}
	parsed, err := logquery.Parse(raw)
	if err != nil {
		writeQueryErr(w, err)
		return
	}
	where, args, err := parsed.SQL()
	if err != nil {
		writeQueryErr(w, err)
		return
	}
	window := time.Duration(qInt(r, "window_seconds", 300, 7*86400)) * time.Second
	to := time.Now().UTC().Add(-u.cfg.AlertingEvalDelay)
	from := to.Add(-window)
	n, err := u.store.CountLogs(r.Context(), clickhouse.LogQuery{
		ProjectID: auth.ProjectIDFromContext(r.Context()), From: from, To: to, Where: where, Args: args,
		Service: qs.Get("service"), Env: qs.Get("env"), HTTPRoute: qs.Get("http_route"), HTTPMethod: qs.Get("http_method"), Limit: 1,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": n, "window_start": from, "window_end": to})
}

// normalizeRecapRule checks a recap rule's send hour and time zone and fills the fields recaps
// don't use. It returns an error code and message, or "" when the rule is fine.
func normalizeRecapRule(rule *api.AlertRule) (string, string) {
	if rule.SendHour > 23 {
		return "bad_send_hour", "send_hour must be 0-23"
	}
	rule.Timezone = strings.TrimSpace(rule.Timezone)
	if rule.Timezone == "" {
		rule.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(rule.Timezone); err != nil {
		return "bad_timezone", "unknown time zone " + strconv.Quote(rule.Timezone)
	}
	rule.TimeWindowSeconds, rule.CooldownSeconds, rule.Threshold = 86400, 86400, 0
	return "", ""
}

// apiAlertRecapPreview builds the recap a rule would send now, for yesterday ("Test this alert").
func (u *UI) apiAlertRecapPreview(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	rule := api.AlertRule{ProjectID: auth.ProjectIDFromContext(r.Context()), Name: strings.TrimSpace(qs.Get("name")), ConditionType: "daily_recap",
		Service: qs.Get("service"), Env: qs.Get("env"), Timezone: qs.Get("timezone")}
	if code, msg := normalizeRecapRule(&rule); code != "" {
		writeErr(w, http.StatusBadRequest, code, msg)
		return
	}
	if u.ch == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable", "recap preview needs the database")
		return
	}
	text, err := alerting.PreviewRecap(r.Context(), u.cfg, u.ch, rule, time.Now().UTC())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "query_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": text})
}
