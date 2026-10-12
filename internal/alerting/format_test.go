package alerting

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"pyloggle/internal/api"
	"pyloggle/internal/config"
)

func TestWords(t *testing.T) {
	for d, want := range map[time.Duration]string{time.Minute: "1 minute", 5 * time.Minute: "5 minutes", time.Hour: "1 hour", 2 * time.Hour: "2 hours",
		24 * time.Hour: "1 day", 90 * time.Second: "90 seconds"} {
		if got := durationWords(d); got != want {
			t.Fatalf("durationWords(%s) = %q, want %q", d, got, want)
		}
	}
	for v, want := range map[float64]string{0.14: "0.14", 6: "6", 2.5: "2.5", 8.4: "8.4", 123.4: "123", 12.25: "12.2"} {
		if got := num(v); got != want {
			t.Fatalf("num(%v) = %q, want %q", v, got, want)
		}
	}
	if countWords(1601) != "1,601" || countWords(1234567) != "1,234,567" || timesWords(1) != "once" || plural(1, "person", "people") != "1 person" {
		t.Fatal("count words")
	}
	if got := scopeWords(api.AlertRule{}); got != "all apps · all environments" {
		t.Fatalf("scope: %q", got)
	}
	if got := scopeWords(api.AlertRule{Service: "api", HTTPMethod: "POST", HTTPRoute: "/pay"}); got != "api · all environments · POST /pay" {
		t.Fatalf("scope: %q", got)
	}
	day := time.Date(2026, 10, 11, 11, 13, 0, 0, time.UTC)
	if got := windowWords(AlertEvent{Kind: "issue_spike", WindowStart: day, WindowEnd: day.Add(time.Hour)}); got != "Oct 11, 11:13–12:13 UTC (1 hour), compared with the 1 hour before" {
		t.Fatalf("window: %q", got)
	}
	late := time.Date(2026, 10, 11, 23, 58, 0, 0, time.UTC)
	if got := windowWords(AlertEvent{Kind: "error_rate", WindowStart: late, WindowEnd: late.Add(5 * time.Minute)}); got != "Oct 11 23:58 – Oct 12 00:03 UTC (5 minutes)" {
		t.Fatalf("window across midnight: %q", got)
	}
	if u, per := rateUnit(0.001); u != "an hour" || per != 3600 {
		t.Fatalf("tiny limits read per hour: %s %v", u, per)
	}
}

func TestHeadlinesAndTitles(t *testing.T) {
	w0 := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	base := AlertEvent{WindowStart: w0, WindowEnd: w0.Add(5 * time.Minute)}
	cases := []struct {
		ev           func(AlertEvent) AlertEvent
		title, lines string
	}{
		{func(e AlertEvent) AlertEvent {
			e.Kind, e.Issues = "new_issue", []IssueRef{{Title: "KeyError: 'sku'"}}
			return e
		}, "A new problem appeared", "First seen: KeyError: 'sku'"},
		{func(e AlertEvent) AlertEvent {
			e.Kind, e.Issues = "regression", []IssueRef{{Title: "a"}, {Title: "b"}}
			return e
		}, "Fixed problems came back", "2 problems marked as fixed happened again in 5 minutes."},
		{func(e AlertEvent) AlertEvent {
			e.Kind, e.Count, e.Rule = "log_query", 0, api.AlertRule{Comparison: "lt", Threshold: 1}
			return e
		}, "Fewer messages than expected", "0 messages matched in 5 minutes. This alert is for fewer than 1."},
		{func(e AlertEvent) AlertEvent {
			e.Kind, e.Spikes = "issue_spike", []IssueSpike{{Title: "TimeoutError", CurCount: 1601, PrevCount: 0}}
			return e
		}, "A problem suddenly jumped", "TimeoutError happened 1,601 times in 5 minutes, up from 0."},
	}
	for _, c := range cases {
		ev := c.ev(base)
		if got := kindTitle(ev); got != c.title {
			t.Fatalf("title = %q, want %q", got, c.title)
		}
		if got := headline(ev); got != c.lines {
			t.Fatalf("headline = %q, want %q", got, c.lines)
		}
	}
}

func richMessage(issues int) Message {
	m := Message{Kind: "issue_spike", Title: "A problem suddenly jumped", Rule: "Checkout <errors> & co", Headline: "PaymentDeclined happened 60 times 🔥",
		Scope: "api · prod", Window: "Oct 11, 11:13–11:18 UTC (5 minutes)", URL: "https://pl.example/ui/issues?status=open&from=x"}
	for i := 0; i < issues; i++ {
		m.Issues = append(m.Issues, MessageIssue{Title: fmt.Sprintf("Problem %d <b>not bold</b> 🔥🔥", i), Badge: "20× more", Counts: "60 times in 5 minutes · 15 people",
			Where: "app/gateway.py, line 41, in call", Code: "if a < b && c > d: raise X()", Context: "api · prod · version 1.4.0 · POST /pay → 500",
			Steps: []string{strings.Repeat("step 🔥 ", 20), strings.Repeat("more ", 30)}, URL: fmt.Sprintf("https://pl.example/ui/issues/i%d?event=e&x=1", i),
			LogsURL: "https://pl.example/ui/logs?q=request%3Ar&from=a", TraceURL: "https://pl.example/ui/traces/t?at=b"})
	}
	return m
}

func TestTelegramHTMLIsEscapedAndFits(t *testing.T) {
	text, mode := telegramText(richMessage(2), true)
	if mode != "HTML" {
		t.Fatalf("mode %q", mode)
	}
	for _, want := range []string{"📈 <b>A problem suddenly jumped</b> · Checkout &lt;errors&gt; &amp; co", `<a href="https://pl.example/ui/issues/i0?event=e&amp;x=1">Problem 0 &lt;b&gt;not bold&lt;/b&gt; 🔥🔥</a>`,
		"<code>if a &lt; b &amp;&amp; c &gt; d: raise X()</code>", "Just before:", `<a href="https://pl.example/ui/traces/t?at=b">Request timeline</a>`, "Open in Pyloggle</a>"} {
		if !strings.Contains(text, want) {
			t.Fatalf("telegram text lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "<b>not bold</b>") {
		t.Fatal("titles must be escaped")
	}

	// Too long for one message: the steps, code and extra problems go first, and it still fits.
	text, mode = telegramText(richMessage(40), true)
	if mode != "HTML" || utf16Len(text) > telegramLimit {
		t.Fatalf("long message: mode %q, %d UTF-16 units", mode, utf16Len(text))
	}
	if !strings.Contains(text, "more problems") {
		t.Fatalf("dropped problems should be counted:\n%s", text)
	}
	// Even when nothing can be dropped, the plain fallback is cut to the limit on a character boundary.
	huge := richMessage(0)
	huge.Scope = strings.Repeat("🔥", 5000)
	text, mode = telegramText(huge, true)
	if mode != "" || utf16Len(text) > telegramLimit || !utf8.ValidString(text) || !strings.HasSuffix(text, "…") {
		t.Fatalf("hard cut: mode %q, %d units, valid %v", mode, utf16Len(text), utf8.ValidString(text))
	}
}

func TestTelegramFallsBackToPlainTextOnce(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/botTOKEN123/sendMessage" {
			http.NotFound(w, r)
			return
		}
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		mu.Lock()
		bodies = append(bodies, b)
		mu.Unlock()
		if b["parse_mode"] == "HTML" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities: Unsupported start tag \"x\" at byte offset 3"}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	n := NewTelegramNotifier(config.Config{AlertTelegramBotToken: "TOKEN123", AlertTelegramAPIURL: srv.URL})
	ev := AlertEvent{Rule: api.AlertRule{Name: "x", TelegramChatIDs: []string{"42"}}, Kind: "new_issue", Issues: []IssueRef{{Title: "boom"}}}
	if err := n.Notify(context.Background(), ev); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if len(bodies) != 2 || bodies[0]["parse_mode"] != "HTML" || bodies[1]["parse_mode"] != nil {
		t.Fatalf("expected HTML then plain: %v", bodies)
	}
	for _, b := range bodies {
		if lp, _ := b["link_preview_options"].(map[string]any); lp["is_disabled"] != true {
			t.Fatalf("link previews must be off: %v", b)
		}
	}
	if !strings.Contains(bodies[1]["text"].(string), "First seen: boom") {
		t.Fatalf("plain text: %v", bodies[1]["text"])
	}

	// Plain mode by setting; another refusal is not retried.
	bodies = nil
	n = NewTelegramNotifier(config.Config{AlertTelegramBotToken: "TOKEN123", AlertTelegramAPIURL: srv.URL, AlertTelegramParseMode: "none"})
	if err := n.Notify(context.Background(), ev); err != nil || len(bodies) != 1 || bodies[0]["parse_mode"] != nil {
		t.Fatalf("parse mode none: %v %v", err, bodies)
	}
}

func TestTelegramErrorsNeverShowTheToken(t *testing.T) {
	n := NewTelegramNotifier(config.Config{AlertTelegramBotToken: "SECRET-TOKEN", AlertTelegramAPIURL: "http://127.0.0.1:1", AlertTelegramTimeout: time.Second})
	err := n.Notify(context.Background(), AlertEvent{Rule: api.AlertRule{TelegramChatIDs: []string{"1"}}, Kind: "new_issue"})
	if err == nil || strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Fatalf("error: %v", err)
	}
	if !telegramUsesHTML("") || telegramUsesHTML("none") || !telegramUsesHTML("MarkdownV2") {
		t.Fatal("parse mode setting")
	}
}

func TestSlackIsEscapedAndWithinLimits(t *testing.T) {
	m := richMessage(2)
	m.Title = strings.Repeat("é", 200)
	p := renderSlack(m)
	blocks := p["blocks"].([]map[string]any)
	header := blocks[0]["text"].(map[string]any)["text"].(string)
	if n := utf8.RuneCountInString(header); n > 150 || !utf8.ValidString(header) {
		t.Fatalf("header: %d runes, valid %v", n, utf8.ValidString(header))
	}
	b, _ := json.Marshal(p)
	s := string(b)
	for _, want := range []string{"Problem 0 \\u0026lt;b\\u0026gt;not bold", "\\u003chttps://pl.example/ui/issues/i0?event=e\\u0026x=1|", "`if a \\u0026lt; b \\u0026amp;\\u0026amp; c \\u0026gt; d: raise X()`"} {
		if !strings.Contains(s, want) {
			t.Fatalf("slack payload lacks %s:\n%s", want, s)
		}
	}
	if strings.Contains(s, "\\u003cb\\u003enot bold") {
		t.Fatal("raw < reached Slack")
	}
}

func TestEmailHeadersAndBody(t *testing.T) {
	m := buildMessage(AlertEvent{Rule: api.AlertRule{Name: "Checkout\r\nBcc: evil@example.com", Service: "api"}, Kind: "new_issue", Issues: []IssueRef{{Title: "Ünïcode"}}})
	msg := buildRFC822("alerts@example.com", []string{"oncall@example.com\r\nBcc: x@y"}, emailSubject(m), renderPlain(m))
	head, body, _ := strings.Cut(msg, "\r\n\r\n")
	for _, l := range strings.Split(head, "\r\n") {
		if strings.HasPrefix(l, "Bcc") {
			t.Fatalf("header injection: %q", head)
		}
	}
	if !strings.Contains(head, "Subject: =?utf-8?q?") || !strings.Contains(head, "Content-Transfer-Encoding: quoted-printable") {
		t.Fatalf("headers: %s", head)
	}
	dec, err := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	if err != nil || !strings.Contains(string(dec), "First seen: Ünïcode") || !strings.Contains(string(dec), "Where: api · all environments") {
		t.Fatalf("body: %v %q", err, dec)
	}
}

// smtpSink is a minimal SMTP server that keeps the DATA of each message.
func smtpSink(t *testing.T) (host string, port int, got func() string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	var mu sync.Mutex
	var data strings.Builder
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		rd := bufio.NewReader(conn)
		say := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }
		say("220 sink")
		inData := false
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if line == ".\r\n" {
					inData = false
					say("250 queued")
					continue
				}
				mu.Lock()
				data.WriteString(strings.TrimPrefix(line, "."))
				mu.Unlock()
				continue
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				say("250 sink")
			case cmd == "DATA":
				inData = true
				say("354 go on")
			case cmd == "QUIT":
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()
	addr := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, func() string {
		mu.Lock()
		defer mu.Unlock()
		return data.String()
	}
}

func TestEmailIsDeliveredOverSMTP(t *testing.T) {
	host, port, got := smtpSink(t)
	n := NewEmailNotifier(config.Config{SMTPHost: host, SMTPPort: port, SMTPFrom: "alerts@example.com"})
	ev := AlertEvent{Rule: api.AlertRule{Name: "Checkout errors", Service: "api", NotificationEmails: []string{"oncall@example.com"}}, Kind: "regression",
		Issues: []IssueRef{{Title: "KeyError: 'sku' — again", Count: 12, Users: 3, URL: "https://pl.example/ui/issues/i1"}}}
	if err := n.Notify(context.Background(), ev); err != nil {
		t.Fatalf("notify: %v", err)
	}
	msg := got()
	head, body, _ := strings.Cut(msg, "\r\n\r\n")
	if !strings.Contains(head, "Subject: =?utf-8?q?[Pyloggle]_A_fixed_problem_came_back:_Checkout_errors") {
		t.Fatalf("subject: %s", head)
	}
	dec, _ := io.ReadAll(quotedprintable.NewReader(strings.NewReader(body)))
	for _, want := range []string{"Marked as fixed, but it happened again: KeyError: 'sku' — again", "1. KeyError: 'sku' — again [Came back]", "Happened 12 times so far · 3 people",
		"Details: https://pl.example/ui/issues/i1", `Sent by Pyloggle because of the alert rule "Checkout errors".`} {
		if !strings.Contains(string(dec), want) {
			t.Fatalf("body lacks %q:\n%s", want, dec)
		}
	}
}
