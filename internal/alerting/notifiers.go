package alerting

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"pyloggle/internal/config"
)

type Notifier interface {
	Name() string
	Notify(ctx context.Context, ev AlertEvent) error
}

// --- Webhook ---

type WebhookNotifier struct {
	client *http.Client
}

func NewWebhookNotifier(cfg config.Config) *WebhookNotifier {
	to := cfg.AlertWebhookTimeout
	if to <= 0 {
		to = 5 * time.Second
	}
	return &WebhookNotifier{
		client: &http.Client{Timeout: to},
	}
}

func (n *WebhookNotifier) Name() string { return "webhook" }

func (n *WebhookNotifier) Notify(ctx context.Context, ev AlertEvent) error {
	urls := ev.Rule.WebhookURLs
	if len(urls) == 0 && contains(ev.Rule.NotificationChannels, "webhook") {
		return errors.New("webhook channel enabled but no webhook_urls configured")
	}
	if len(urls) == 0 {
		return nil
	}

	body, _ := json.Marshal(ev)
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
				firstErr = fmt.Errorf("non-2xx from %s: %s", u, resp.Status)
			}
			continue
		}
	}
	return firstErr
}

// --- Email (SMTP) ---

type EmailNotifier struct {
	cfg config.Config
}

func NewEmailNotifier(cfg config.Config) *EmailNotifier {
	return &EmailNotifier{cfg: cfg}
}

func (n *EmailNotifier) Name() string { return "email" }

func (n *EmailNotifier) Notify(ctx context.Context, ev AlertEvent) error {
	_ = ctx // SMTP client doesn't support contexts; keep signature consistent.
	if n.cfg.SMTPHost == "" {
		return nil
	}

	to := ev.Rule.NotificationEmails
	if len(to) == 0 && contains(ev.Rule.NotificationChannels, "email") {
		return errors.New("email channel enabled but no notification_emails configured")
	}
	if len(to) == 0 {
		return nil
	}

	from := strings.TrimSpace(n.cfg.SMTPFrom)
	if from == "" {
		from = strings.TrimSpace(n.cfg.SMTPUsername)
	}
	if from == "" {
		return errors.New("missing ALERT_SMTP_FROM (or ALERT_SMTP_USERNAME) for email notifier")
	}

	subject := fmt.Sprintf("[pyloggle] %s: %s (%s/%s)", ev.Kind, safe(ev.Rule.Name), safe(ev.Rule.Service), safe(ev.Rule.Env))
	if ev.Rule.HTTPRoute != "" {
		subject += " " + ev.Rule.HTTPRoute
	}

	plain := renderPlain(ev)
	msg := buildRFC822(from, to, subject, plain)

	addr := net.JoinHostPort(n.cfg.SMTPHost, fmt.Sprintf("%d", n.cfg.SMTPPort))
	c, err := dialSMTP(n.cfg, addr)
	if err != nil {
		return err
	}
	defer func() { _ = c.Quit() }()

	if n.cfg.SMTPUsername != "" {
		auth := smtp.PlainAuth("", n.cfg.SMTPUsername, n.cfg.SMTPPassword, n.cfg.SMTPHost)
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		rcpt = strings.TrimSpace(rcpt)
		if rcpt == "" {
			continue
		}
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func dialSMTP(cfg config.Config, addr string) (*smtp.Client, error) {
	if cfg.SMTPTLS {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: cfg.SMTPHost})
		if err != nil {
			return nil, err
		}
		return smtp.NewClient(conn, cfg.SMTPHost)
	}

	c, err := smtp.Dial(addr)
	if err != nil {
		return nil, err
	}
	// Opportunistic STARTTLS if supported.
	if ok, _ := c.Extension("STARTTLS"); ok {
		_ = c.StartTLS(&tls.Config{ServerName: cfg.SMTPHost})
	}
	return c, nil
}

func buildRFC822(from string, to []string, subject string, body string) string {
	// Very small RFC822-ish email. Good enough for most SMTP relays.
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.Join(to, ", ") + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

func renderPlain(ev AlertEvent) string {
	var b strings.Builder
	b.WriteString("Pyloggle alert fired\n")
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("Rule: %s (%s)\n", ev.Rule.Name, ev.Rule.ID))
	b.WriteString(fmt.Sprintf("Project: %s\n", ev.Rule.ProjectID))
	b.WriteString(fmt.Sprintf("Service/Env: %s / %s\n", ev.Rule.Service, ev.Rule.Env))
	if ev.Rule.HTTPRoute != "" || ev.Rule.HTTPMethod != "" {
		b.WriteString(fmt.Sprintf("HTTP: %s %s\n", ev.Rule.HTTPMethod, ev.Rule.HTTPRoute))
	}
	b.WriteString(fmt.Sprintf("Kind: %s\n", ev.Kind))
	b.WriteString(fmt.Sprintf("Window: %s .. %s\n", ev.WindowStart.UTC().Format(time.RFC3339), ev.WindowEnd.UTC().Format(time.RFC3339)))
	b.WriteString("\n")
	switch ev.Kind {
	case "error_rate":
		b.WriteString(fmt.Sprintf("Error count: %d\n", ev.ErrorCount))
		b.WriteString(fmt.Sprintf("Error rate: %.4f errors/sec (threshold %.4f)\n", ev.ErrorRate, ev.Rule.Threshold))
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
	}
	return b.String()
}

func contains(ss []string, want string) bool {
	want = strings.ToLower(strings.TrimSpace(want))
	for _, s := range ss {
		if strings.ToLower(strings.TrimSpace(s)) == want {
			return true
		}
	}
	return false
}

func safe(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	return s
}
