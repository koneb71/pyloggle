package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pyloggle/internal/config"
)

// SlackNotifier posts Block Kit messages to incoming webhooks: the rule's own URLs, or the
// server-wide ALERT_SLACK_WEBHOOK_URL when the rule only lists the "slack" channel.
type SlackNotifier struct {
	client     *http.Client
	defaultURL string
}

func NewSlackNotifier(cfg config.Config) *SlackNotifier {
	to := cfg.AlertWebhookTimeout
	if to <= 0 {
		to = 5 * time.Second
	}
	return &SlackNotifier{client: &http.Client{Timeout: to}, defaultURL: strings.TrimSpace(cfg.AlertSlackWebhookURL)}
}

func (n *SlackNotifier) Name() string { return "slack" }

func (n *SlackNotifier) Notify(ctx context.Context, ev AlertEvent) error {
	urls := ev.Rule.SlackWebhookURLs
	if len(urls) == 0 && contains(ev.Rule.NotificationChannels, "slack") {
		if n.defaultURL == "" {
			return errors.New("slack channel enabled but no slack_webhook_urls configured and ALERT_SLACK_WEBHOOK_URL is unset")
		}
		urls = []string{n.defaultURL}
	}
	if len(urls) == 0 {
		return nil
	}
	body, _ := json.Marshal(SlackPayload(ev))
	var firstErr error
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := n.client.Do(req)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if firstErr == nil {
				firstErr = fmt.Errorf("slack webhook %s: %s", u, resp.Status)
			}
		}
	}
	return firstErr
}

// SlackPayload renders an event as a Block Kit message with a plain-text fallback.
func SlackPayload(ev AlertEvent) map[string]any {
	title := fmt.Sprintf("Pyloggle: %s — %s", kindLabel(ev.Kind), safe(ev.Rule.Name))
	if len(title) > 150 {
		title = title[:147] + "..."
	}
	scope := strings.TrimSpace(safe(ev.Rule.Service) + " / " + safe(ev.Rule.Env))
	if ev.Rule.HTTPRoute != "" || ev.Rule.HTTPMethod != "" {
		scope += " · " + strings.TrimSpace(ev.Rule.HTTPMethod+" "+ev.Rule.HTTPRoute)
	}
	fields := []map[string]any{
		mrkdwn("*Project*\n" + safe(ev.Rule.ProjectID)),
		mrkdwn("*Scope*\n" + scope),
		mrkdwn(fmt.Sprintf("*Window*\n%s → %s", ev.WindowStart.UTC().Format("15:04:05"), ev.WindowEnd.UTC().Format("15:04:05 MST"))),
		mrkdwn("*Value*\n" + valueLine(ev)),
	}
	blocks := []map[string]any{
		{"type": "header", "text": map[string]any{"type": "plain_text", "text": title, "emoji": false}},
		{"type": "section", "fields": fields},
	}
	if ev.Kind == "log_query" && ev.Rule.Query != "" {
		blocks = append(blocks, map[string]any{"type": "section", "text": mrkdwn("*Query*\n`" + ev.Rule.Query + "`")})
	}
	if lines := issueLines(ev); len(lines) > 0 {
		blocks = append(blocks, map[string]any{"type": "section", "text": mrkdwn(strings.Join(lines, "\n"))})
	}
	if ev.URL != "" {
		blocks = append(blocks, map[string]any{"type": "actions", "elements": []map[string]any{
			{"type": "button", "text": map[string]any{"type": "plain_text", "text": "Open in Pyloggle"}, "url": ev.URL},
		}})
	}
	return map[string]any{"text": title + " · " + valueLine(ev), "blocks": blocks}
}

func mrkdwn(s string) map[string]any { return map[string]any{"type": "mrkdwn", "text": s} }

func kindLabel(kind string) string {
	switch kind {
	case "error_rate":
		return "error rate"
	case "issue_spike":
		return "issue spike"
	case "new_issue":
		return "new issue"
	case "regression":
		return "regression"
	case "log_query":
		return "log monitor"
	}
	return kind
}

func valueLine(ev AlertEvent) string {
	switch ev.Kind {
	case "error_rate":
		return fmt.Sprintf("%d errors, %.3f/s (threshold %.3f/s)", ev.ErrorCount, ev.ErrorRate, ev.Rule.Threshold)
	case "issue_spike":
		return fmt.Sprintf("%d issue(s) spiked by ≥ %.0f", len(ev.Spikes), ev.Rule.Threshold)
	case "new_issue":
		return fmt.Sprintf("%d new issue(s)", len(ev.Issues))
	case "regression":
		return fmt.Sprintf("%d regressed issue(s)", len(ev.Issues))
	case "log_query":
		return fmt.Sprintf("%d matching lines (%s %.0f)", ev.Count, comparisonSymbol(ev.Rule.Comparison), ev.Rule.Threshold)
	}
	return ""
}

func comparisonSymbol(op string) string {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "gte":
		return "≥"
	case "lt":
		return "<"
	case "lte":
		return "≤"
	case "eq":
		return "="
	}
	return ">"
}

func issueLines(ev AlertEvent) []string {
	var lines []string
	link := func(u, text string) string {
		if u == "" {
			return text
		}
		return "<" + u + "|" + text + ">"
	}
	for i, s := range ev.Spikes {
		if i >= 10 {
			break
		}
		lines = append(lines, fmt.Sprintf("• %s — %d now, %d before", link(s.URL, s.Title), s.CurCount, s.PrevCount))
	}
	for i, is := range ev.Issues {
		if i >= 10 {
			break
		}
		l := "• " + link(is.URL, is.Title)
		if is.Culprit != "" {
			l += " _" + is.Culprit + "_"
		}
		if is.Count > 0 {
			l += fmt.Sprintf(" ×%d", is.Count)
		}
		lines = append(lines, l)
	}
	return lines
}
