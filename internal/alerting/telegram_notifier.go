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

type TelegramNotifier struct {
	token     string
	parseMode string
	client    *http.Client
}

func NewTelegramNotifier(cfg config.Config) *TelegramNotifier {
	to := cfg.AlertTelegramTimeout
	if to <= 0 {
		to = 5 * time.Second
	}
	return &TelegramNotifier{
		token:     strings.TrimSpace(cfg.AlertTelegramBotToken),
		parseMode: strings.TrimSpace(cfg.AlertTelegramParseMode),
		client:    &http.Client{Timeout: to},
	}
}

func (n *TelegramNotifier) Name() string { return "telegram" }

func (n *TelegramNotifier) Notify(ctx context.Context, ev AlertEvent) error {
	if n.token == "" {
		return nil
	}
	chatIDs := ev.Rule.TelegramChatIDs
	if len(chatIDs) == 0 && contains(ev.Rule.NotificationChannels, "telegram") {
		return errors.New("telegram channel enabled but no telegram_chat_ids configured")
	}
	if len(chatIDs) == 0 {
		return nil
	}

	text := renderTelegram(ev)

	var firstErr error
	for _, chatID := range chatIDs {
		chatID = strings.TrimSpace(chatID)
		if chatID == "" {
			continue
		}
		if err := n.sendMessage(ctx, chatID, text); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (n *TelegramNotifier) sendMessage(ctx context.Context, chatID string, text string) error {
	u := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", n.token)
	reqBody := map[string]any{
		"chat_id": chatID,
		"text":    text,
	}
	if n.parseMode != "" {
		reqBody["parse_mode"] = n.parseMode
	}
	b, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("telegram sendMessage failed: %s", resp.Status)
	}
	return nil
}

func renderTelegram(ev AlertEvent) string {
	// Keep it plaintext by default (parse_mode optional).
	var b strings.Builder
	b.WriteString("Pyloggle alert fired\n")
	b.WriteString(fmt.Sprintf("Kind: %s\n", ev.Kind))
	b.WriteString(fmt.Sprintf("Rule: %s (%s)\n", safe(ev.Rule.Name), ev.Rule.ID))
	b.WriteString(fmt.Sprintf("Project: %s\n", safe(ev.Rule.ProjectID)))
	b.WriteString(fmt.Sprintf("Service/Env: %s / %s\n", safe(ev.Rule.Service), safe(ev.Rule.Env)))
	if ev.Rule.HTTPRoute != "" || ev.Rule.HTTPMethod != "" {
		b.WriteString(fmt.Sprintf("HTTP: %s %s\n", strings.TrimSpace(ev.Rule.HTTPMethod), strings.TrimSpace(ev.Rule.HTTPRoute)))
	}
	b.WriteString(fmt.Sprintf("Window: %s .. %s\n",
		ev.WindowStart.UTC().Format(time.RFC3339),
		ev.WindowEnd.UTC().Format(time.RFC3339),
	))

	switch ev.Kind {
	case "error_rate":
		b.WriteString(fmt.Sprintf("Errors: %d\n", ev.ErrorCount))
		b.WriteString(fmt.Sprintf("Rate: %.4f errors/sec (threshold %.4f)\n", ev.ErrorRate, ev.Rule.Threshold))
	case "issue_spike":
		b.WriteString(fmt.Sprintf("Issue spikes (delta >= %.0f):\n", ev.Rule.Threshold))
		for _, s := range ev.Spikes {
			b.WriteString(fmt.Sprintf("- %s: %s (cur=%d prev=%d delta=%d)\n", s.IssueID, s.Title, s.CurCount, s.PrevCount, s.Delta))
		}
	case "new_issue":
		b.WriteString("New issues:\n")
		for _, is := range ev.Issues {
			b.WriteString(fmt.Sprintf("- %s (%s) x%d %s\n", is.Title, is.Culprit, is.Count, is.URL))
		}
	case "regression":
		b.WriteString("Regressed issues (resolved, now back):\n")
		for _, is := range ev.Issues {
			b.WriteString(fmt.Sprintf("- %s (%s) x%d %s\n", is.Title, is.Culprit, is.Count, is.URL))
		}
	case "log_query":
		b.WriteString(fmt.Sprintf("Query: %s\n", ev.Rule.Query))
		b.WriteString(fmt.Sprintf("Matched: %d lines (%s %.0f)\n", ev.Count, comparisonSymbol(ev.Rule.Comparison), ev.Rule.Threshold))
	}
	if ev.URL != "" {
		b.WriteString("Open: " + ev.URL + "\n")
	}
	return b.String()
}
