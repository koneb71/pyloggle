package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/koneb71/pyloggle/internal/config"
)

// TelegramNotifier sends messages through the Bot API. Messages are HTML by default
// (ALERT_TELEGRAM_PARSE_MODE=none sends plain text); a message Telegram cannot parse is sent
// again once as plain text.
type TelegramNotifier struct {
	token   string
	apiURL  string
	useHTML bool
	client  *http.Client
}

func NewTelegramNotifier(cfg config.Config) *TelegramNotifier {
	to := cfg.AlertTelegramTimeout
	if to <= 0 {
		to = 5 * time.Second
	}
	api := strings.TrimRight(strings.TrimSpace(cfg.AlertTelegramAPIURL), "/")
	if api == "" {
		api = "https://api.telegram.org"
	}
	return &TelegramNotifier{
		token:   strings.TrimSpace(cfg.AlertTelegramBotToken),
		apiURL:  api,
		useHTML: telegramUsesHTML(cfg.AlertTelegramParseMode),
		client:  &http.Client{Timeout: to},
	}
}

// telegramUsesHTML reads ALERT_TELEGRAM_PARSE_MODE: HTML (the default) or none. Messages are
// built as HTML, so an old MarkdownV2 / Markdown setting also means HTML.
func telegramUsesHTML(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "html":
		return true
	case "none", "plain", "text", "off":
		return false
	case "markdown", "markdownv2":
		log.Printf("alerting: ALERT_TELEGRAM_PARSE_MODE=%s is not supported any more; sending HTML (set it to none for plain text)", mode)
		return true
	}
	log.Printf("alerting: unknown ALERT_TELEGRAM_PARSE_MODE=%q; sending HTML", mode)
	return true
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

	m := buildMessage(ev)
	text, mode := telegramText(m, n.useHTML)

	var firstErr error
	for _, chatID := range chatIDs {
		chatID = strings.TrimSpace(chatID)
		if chatID == "" {
			continue
		}
		err := n.sendMessage(ctx, chatID, text, mode)
		var te *telegramError
		if err != nil && mode != "" && errors.As(err, &te) && te.isParseError() {
			log.Printf("alerting: telegram could not read the formatted message (%s); sending plain text", te.Description)
			err = n.sendMessage(ctx, chatID, telegramPlain(m), "")
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// telegramError is a Bot API refusal.
type telegramError struct {
	Status      int
	Description string
}

func (e *telegramError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("telegram sendMessage failed: %d %s", e.Status, e.Description)
	}
	return fmt.Sprintf("telegram sendMessage failed: %d", e.Status)
}

func (e *telegramError) isParseError() bool {
	d := strings.ToLower(e.Description)
	return e.Status == http.StatusBadRequest && (strings.Contains(d, "parse") || strings.Contains(d, "entit") || strings.Contains(d, "tag"))
}

func (n *TelegramNotifier) sendMessage(ctx context.Context, chatID, text, parseMode string) error {
	reqBody := map[string]any{
		"chat_id":              chatID,
		"text":                 text,
		"link_preview_options": map[string]any{"is_disabled": true},
	}
	if parseMode != "" {
		reqBody["parse_mode"] = parseMode
	}
	b, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.apiURL+"/bot"+n.token+"/sendMessage", bytes.NewReader(b))
	if err != nil {
		return errors.New("telegram sendMessage: bad API URL")
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		// The request URL holds the bot token; never let it reach the logs.
		var ue *url.Error
		if errors.As(err, &ue) {
			return fmt.Errorf("telegram sendMessage: %w", ue.Err)
		}
		return errors.New("telegram sendMessage failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil
	}
	var body struct {
		Description string `json:"description"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&body)
	return &telegramError{Status: resp.StatusCode, Description: body.Description}
}
