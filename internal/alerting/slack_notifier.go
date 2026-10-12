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

	"github.com/koneb71/pyloggle/internal/config"
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
func SlackPayload(ev AlertEvent) map[string]any { return renderSlack(buildMessage(ev)) }
