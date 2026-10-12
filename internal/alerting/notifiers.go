package alerting

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/smtp"
	"strings"
	"time"

	"github.com/koneb71/pyloggle/internal/config"
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

	m := buildMessage(ev)
	msg := buildRFC822(from, to, emailSubject(m), renderPlain(m)+"\n--\nSent by Pyloggle because of the alert rule \""+safe(ev.Rule.Name)+"\".\n")

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

// emailSubject is "[Pyloggle] Too many errors: Checkout errors (api · prod)" on one line
// (buildRFC822 encodes it).
func emailSubject(m Message) string {
	name := m.Rule
	if name == "" {
		name = m.Headline
	}
	return clip(oneLine(fmt.Sprintf("[Pyloggle] %s: %s (%s)", m.Title, name, m.Scope)), 200)
}

// encodeHeader removes line breaks (no header injection) and encodes non-ASCII text.
func encodeHeader(s string) string {
	s = strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
	for _, r := range s {
		if r > 126 || r < 32 {
			return mime.QEncoding.Encode("utf-8", s)
		}
	}
	return s
}

// buildRFC822 writes a small UTF-8 plain-text message with a quoted-printable body.
func buildRFC822(from string, to []string, subject string, body string) string {
	clean := strings.NewReplacer("\r", "", "\n", "").Replace
	rcpts := make([]string, 0, len(to))
	for _, t := range to {
		if t = clean(strings.TrimSpace(t)); t != "" {
			rcpts = append(rcpts, t)
		}
	}
	var b strings.Builder
	b.WriteString("From: " + clean(from) + "\r\n")
	b.WriteString("To: " + strings.Join(rcpts, ", ") + "\r\n")
	b.WriteString("Subject: " + encodeHeader(subject) + "\r\n")
	b.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	b.WriteString("\r\n")
	var qp bytes.Buffer
	w := quotedprintable.NewWriter(&qp)
	_, _ = w.Write([]byte(strings.ReplaceAll(body, "\r\n", "\n")))
	_ = w.Close()
	b.Write(qp.Bytes())
	if !strings.HasSuffix(qp.String(), "\n") {
		b.WriteString("\r\n")
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
