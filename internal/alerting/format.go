package alerting

// One alert, told in words once (Message) and rendered three ways: plain text (e-mail and the
// Telegram fallback), Telegram HTML and Slack Block Kit. The renderers only lay the model out,
// so the channels never disagree about what happened.

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"pyloggle/internal/api"
)

// Message is an alert in plain words.
type Message struct {
	Kind     string // rule kind, for the Telegram marker
	Title    string // "Too many errors"
	Rule     string // the rule's name
	Headline string // what happened, in one or two sentences
	Scope    string // "api · prod" or "all apps · all environments"
	Window   string // "Oct 11, 11:13–11:18 UTC (5 minutes)"
	Query    string // log monitors: the search
	Issues   []MessageIssue
	More     int // problems in the firing that are not listed
	Samples  []LogSample
	Sections []MessageSection // recaps: titled lists
	URL      string           // "Open in Pyloggle"
}

// MessageSection is a titled list of lines (the recap of yesterday).
type MessageSection struct {
	Title string
	Lines []MessageLine
}

// MessageLine is one line of a section: Lead (linked to URL when set) followed by Text.
type MessageLine struct {
	Lead string
	Text string
	URL  string
}

// MessageIssue is one problem in a message. Only Title is always set.
type MessageIssue struct {
	Title    string
	Badge    string // "20× more", "New", "Came back"
	Counts   string // "60 times in 5 minutes, 3 in the 5 minutes before · 15 people"
	Where    string // "app/gateway.py, line 41, in call"
	Code     string // the line that failed
	Context  string // "api · prod · version 1.4.0 · POST /pay → 500"
	Steps    []string
	URL      string
	LogsURL  string
	TraceURL string
}

const listedIssues = 5

// buildMessage puts an event into words.
func buildMessage(ev AlertEvent) Message {
	m := Message{Kind: ev.Kind, Title: kindTitle(ev), Rule: oneLine(ev.Rule.Name), Headline: headline(ev), Scope: scopeWords(ev.Rule),
		Window: windowWords(ev), Samples: ev.Samples, URL: ev.URL}
	if ev.Kind == "log_query" {
		m.Query = ev.Rule.Query
	}
	if ev.Kind == "daily_recap" {
		if ev.Recap != nil {
			m.Sections = recapSections(*ev.Recap)
		}
		return m
	}
	detailed := map[string]IssueContext{}
	for _, ic := range ev.TopIssues {
		detailed[ic.IssueID] = ic
	}
	var list []IssueContext
	pick := func(base IssueContext) {
		if d, ok := detailed[base.IssueID]; ok {
			list = append(list, d)
			return
		}
		list = append(list, base)
	}
	switch ev.Kind {
	case "issue_spike":
		for _, s := range ev.Spikes {
			pick(IssueContext{IssueID: s.IssueID, Title: s.Title, Reason: "jumped", Count: s.CurCount, PrevCount: s.PrevCount, Users: s.Users, URL: s.URL})
		}
	case "new_issue", "regression":
		reason := map[string]string{"new_issue": "new", "regression": "came_back"}[ev.Kind]
		for _, is := range ev.Issues {
			pick(IssueContext{IssueID: is.IssueID, Title: is.Title, Reason: reason, Count: is.Count, Users: is.Users, URL: is.URL})
		}
	default:
		list = ev.TopIssues
	}
	if len(list) > listedIssues {
		m.More = len(list) - listedIssues
		list = list[:listedIssues]
	}
	span := durationWords(ev.WindowEnd.Sub(ev.WindowStart))
	for _, ic := range list {
		m.Issues = append(m.Issues, issueWords(ic, span))
	}
	return m
}

func issueWords(ic IssueContext, span string) MessageIssue {
	mi := MessageIssue{Title: clip(oneLine(ic.Title), 200), Where: ic.Where, Code: ic.Code, Steps: ic.Steps, URL: ic.URL, LogsURL: ic.LogsURL, TraceURL: ic.TraceURL}
	if mi.Title == "" {
		mi.Title = "Untitled problem"
	}
	var counts string
	switch ic.Reason {
	case "jumped", "most_frequent":
		counts = fmt.Sprintf("%s in %s, %s in the %s before", timesWords(ic.Count), span, countWords(ic.PrevCount), span)
		if ic.Reason == "jumped" {
			if ic.PrevCount == 0 {
				mi.Badge = "Up from none"
			} else if ic.Count > ic.PrevCount {
				mi.Badge = num(float64(ic.Count)/float64(ic.PrevCount)) + "× more"
			}
		}
	case "new":
		counts, mi.Badge = "Happened "+timesWords(ic.Count)+" so far", "New"
	case "came_back":
		counts, mi.Badge = "Happened "+timesWords(ic.Count)+" so far", "Came back"
	default:
		if ic.Count > 0 {
			counts = timesWords(ic.Count)
		}
	}
	if ic.Users > 0 {
		counts += " · " + plural(ic.Users, "person", "people")
	}
	mi.Counts = strings.TrimPrefix(counts, " · ")
	var ctx []string
	for _, s := range []string{ic.Service, ic.Env} {
		if s != "" {
			ctx = append(ctx, s)
		}
	}
	if ic.Release != "" {
		ctx = append(ctx, "version "+ic.Release)
	}
	if ic.Request != "" {
		ctx = append(ctx, ic.Request)
	}
	mi.Context = strings.Join(ctx, " · ")
	return mi
}

// kindTitle names what kind of thing happened, in the words the UI uses.
func kindTitle(ev AlertEvent) string {
	many := len(ev.Issues) > 1 || len(ev.Spikes) > 1
	switch ev.Kind {
	case "error_rate":
		return "Too many errors"
	case "issue_spike":
		if many {
			return "Problems suddenly jumped"
		}
		return "A problem suddenly jumped"
	case "new_issue":
		if many {
			return "New problems appeared"
		}
		return "A new problem appeared"
	case "regression":
		if many {
			return "Fixed problems came back"
		}
		return "A fixed problem came back"
	case "log_query":
		if op := strings.ToLower(strings.TrimSpace(ev.Rule.Comparison)); op == "lt" || op == "lte" {
			return "Fewer messages than expected"
		}
		return "Messages matched a search"
	case "daily_recap":
		if ev.Recap != nil && ev.Recap.Project != "" {
			return "Yesterday in " + ev.Recap.Project
		}
		return "Recap of yesterday"
	}
	return strings.ReplaceAll(ev.Kind, "_", " ")
}

// headline says what happened in a sentence or two.
func headline(ev AlertEvent) string {
	span := durationWords(ev.WindowEnd.Sub(ev.WindowStart))
	switch ev.Kind {
	case "daily_recap":
		if ev.Recap != nil {
			return recapHeadline(*ev.Recap)
		}
		return ""
	case "error_rate":
		unit, per := rateUnit(ev.Rule.Threshold)
		return fmt.Sprintf("%s in %s, %s %s. The limit is %s %s.", plural(ev.ErrorCount, "error", "errors"), span,
			num(ev.ErrorRate*per), unit, num(ev.Rule.Threshold*per), unit)
	case "issue_spike":
		if len(ev.Spikes) == 0 {
			return ""
		}
		s := ev.Spikes[0]
		if len(ev.Spikes) == 1 {
			return fmt.Sprintf("%s happened %s in %s, up from %s.", clip(oneLine(s.Title), 120), timesWords(s.CurCount), span, countWords(s.PrevCount))
		}
		return fmt.Sprintf("%d problems happened at least %s more times than in the %s before. The biggest, %s, happened %s, up from %s.", len(ev.Spikes),
			num(ev.Rule.Threshold), span, clip(oneLine(s.Title), 120), timesWords(s.CurCount), countWords(s.PrevCount))
	case "new_issue":
		if len(ev.Issues) == 1 {
			return "First seen: " + clip(oneLine(ev.Issues[0].Title), 160)
		}
		return fmt.Sprintf("%d new problems in %s.", len(ev.Issues), span)
	case "regression":
		if len(ev.Issues) == 1 {
			return "Marked as fixed, but it happened again: " + clip(oneLine(ev.Issues[0].Title), 160)
		}
		return fmt.Sprintf("%d problems marked as fixed happened again in %s.", len(ev.Issues), span)
	case "log_query":
		return fmt.Sprintf("%s matched in %s. This alert is for %s %s.", plural(ev.Count, "message", "messages"), span,
			comparisonWords(ev.Rule.Comparison), num(ev.Rule.Threshold))
	}
	return ""
}

// scopeWords is what the rule watches: "api · prod · POST /pay", or all of it.
func scopeWords(rule api.AlertRule) string {
	service, env := oneLine(rule.Service), oneLine(rule.Env)
	method, route := oneLine(rule.HTTPMethod), oneLine(rule.HTTPRoute)
	if service == "" {
		service = "all apps"
	}
	if env == "" {
		env = "all environments"
	}
	parts := []string{service, env}
	if r := strings.TrimSpace(method + " " + route); r != "" {
		parts = append(parts, r)
	}
	return strings.Join(parts, " · ")
}

// windowWords is "Oct 11, 11:13–11:18 UTC (5 minutes)", plus the comparison for spikes.
func windowWords(ev AlertEvent) string {
	a, b := ev.WindowStart.UTC(), ev.WindowEnd.UTC()
	if a.IsZero() || b.IsZero() {
		return ""
	}
	if ev.Kind == "daily_recap" && ev.Recap != nil {
		if d, err := time.Parse("2006-01-02", ev.Recap.Date); err == nil {
			return d.Format("Monday, Jan 2") + " (" + ev.Recap.Timezone + ")"
		}
	}
	var s string
	if a.Format("2006-01-02") == b.Format("2006-01-02") {
		s = a.Format("Jan 2, 15:04") + "–" + b.Format("15:04") + " UTC"
	} else {
		s = a.Format("Jan 2 15:04") + " – " + b.Format("Jan 2 15:04") + " UTC"
	}
	d := b.Sub(a)
	s += " (" + durationWords(d) + ")"
	if ev.Kind == "issue_spike" {
		s += ", compared with the " + durationWords(d) + " before"
	}
	return s
}

// durationWords: "5 minutes", "1 hour", "90 seconds", "2 days".
func durationWords(d time.Duration) string {
	switch {
	case d <= 0:
		return "0 seconds"
	case d%(24*time.Hour) == 0:
		return plural(uint64(d/(24*time.Hour)), "day", "days")
	case d%time.Hour == 0:
		return plural(uint64(d/time.Hour), "hour", "hours")
	case d%time.Minute == 0:
		return plural(uint64(d/time.Minute), "minute", "minutes")
	}
	return plural(uint64(d.Round(time.Second)/time.Second), "second", "seconds")
}

// rateUnit picks the unit that keeps the rule's limit at or above 1: "a second", "a minute"
// or "an hour", and the factor from errors per second.
func rateUnit(perSecond float64) (string, float64) {
	switch {
	case perSecond >= 1:
		return "a second", 1
	case perSecond*60 >= 1:
		return "a minute", 60
	}
	return "an hour", 3600
}

func comparisonWords(op string) string {
	switch strings.ToLower(strings.TrimSpace(op)) {
	case "gte":
		return "at least"
	case "lt":
		return "fewer than"
	case "lte":
		return "at most"
	case "eq":
		return "exactly"
	}
	return "more than"
}

// num prints a measurement with up to two decimals and no trailing zeros: 0.14, 6, 2.5, 120.
func num(v float64) string {
	prec := 2
	switch {
	case v >= 100:
		prec = 0
	case v >= 10:
		prec = 1
	}
	s := strconv.FormatFloat(v, 'f', prec, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

// countWords prints a count with thousands separators: 1,601.
func countWords(n uint64) string {
	s := strconv.FormatUint(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func timesWords(n uint64) string {
	if n == 1 {
		return "once"
	}
	return countWords(n) + " times"
}

func plural(n uint64, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return countWords(n) + " " + many
}

func levelWord(level string) string {
	switch strings.ToLower(level) {
	case "warn", "warning":
		return "Warning"
	case "fatal", "critical":
		return "Fatal"
	case "":
		return ""
	}
	return strings.ToUpper(level[:1]) + strings.ToLower(level[1:])
}

func sampleWords(s LogSample) string {
	parts := []string{s.TS.UTC().Format("15:04:05")}
	if l := levelWord(s.Level); l != "" {
		parts = append(parts, l)
	}
	if s.Service != "" {
		parts = append(parts, s.Service)
	}
	return strings.Join(parts, " · ") + ": " + s.Message
}

// --- Plain text (e-mail, Telegram fallback) ---

func renderPlain(m Message) string {
	var b strings.Builder
	line := func(s string) {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	head := m.Title
	if m.Rule != "" {
		head += ": " + m.Rule
	}
	line(head)
	line("")
	if m.Headline != "" {
		line(m.Headline)
		line("")
	}
	line("Where: " + m.Scope)
	if m.Window != "" {
		line("When:  " + m.Window)
	}
	if m.Query != "" {
		line("Search: " + m.Query)
	}
	if len(m.Issues) > 0 {
		line("")
		for i, is := range m.Issues {
			t := fmt.Sprintf("%d. %s", i+1, is.Title)
			if is.Badge != "" {
				t += " [" + is.Badge + "]"
			}
			line(t)
			if is.Counts != "" {
				line("   " + is.Counts)
			}
			if is.Where != "" {
				line("   Happened in " + is.Where)
			}
			if is.Code != "" {
				line("     " + is.Code)
			}
			if is.Context != "" {
				line("   " + is.Context)
			}
			if len(is.Steps) > 0 {
				line("   Just before:")
				for _, s := range is.Steps {
					line("     " + s)
				}
			}
			if is.URL != "" {
				line("   Details: " + is.URL)
			}
			if is.LogsURL != "" {
				line("   Messages from this request: " + is.LogsURL)
			}
			if is.TraceURL != "" {
				line("   Request timeline: " + is.TraceURL)
			}
		}
		if m.More > 0 {
			line(fmt.Sprintf("   …and %s", plural(uint64(m.More), "more problem", "more problems")))
		}
	}
	for _, sec := range m.Sections {
		line("")
		line(sec.Title + ":")
		for _, l := range sec.Lines {
			t := "  • " + l.Lead + l.Text
			if l.URL != "" {
				t += "  " + l.URL
			}
			line(t)
		}
	}
	if len(m.Samples) > 0 {
		line("")
		line("Latest matching messages:")
		for _, s := range m.Samples {
			line("  " + sampleWords(s))
		}
	}
	if m.URL != "" {
		line("")
		line("Open in Pyloggle: " + m.URL)
	}
	return b.String()
}

// --- Telegram HTML ---

const telegramLimit = 4096 // UTF-16 code units

var telegramMarker = map[string]string{"error_rate": "🔴", "issue_spike": "📈", "new_issue": "🆕", "regression": "🔁", "log_query": "🔎", "daily_recap": "📋"}

func renderTelegramHTML(m Message) string {
	e := html.EscapeString
	link := func(u, text string) string {
		if u == "" {
			return e(text)
		}
		return `<a href="` + e(u) + `">` + e(text) + `</a>`
	}
	var b strings.Builder
	line := func(s string) {
		b.WriteString(s)
		b.WriteByte('\n')
	}
	head := "<b>" + e(m.Title) + "</b>"
	if mk := telegramMarker[m.Kind]; mk != "" {
		head = mk + " " + head
	}
	if m.Rule != "" {
		head += " · " + e(m.Rule)
	}
	line(head)
	if m.Headline != "" {
		line(e(m.Headline))
	}
	line("")
	line("<b>Where</b> " + e(m.Scope))
	if m.Window != "" {
		line("<b>When</b> " + e(m.Window))
	}
	if m.Query != "" {
		line("<b>Search</b> <code>" + e(m.Query) + "</code>")
	}
	for i, is := range m.Issues {
		line("")
		t := fmt.Sprintf("<b>%d. %s</b>", i+1, link(is.URL, is.Title))
		if is.Badge != "" {
			t += " · <i>" + e(is.Badge) + "</i>"
		}
		line(t)
		if is.Counts != "" {
			line(e(is.Counts))
		}
		if is.Where != "" {
			line("<i>Happened in " + e(is.Where) + "</i>")
		}
		if is.Code != "" {
			line("<code>" + e(is.Code) + "</code>")
		}
		if is.Context != "" {
			line(e(is.Context))
		}
		if len(is.Steps) > 0 {
			line("Just before:")
			for _, s := range is.Steps {
				line("• " + e(s))
			}
		}
		var links []string
		if is.LogsURL != "" {
			links = append(links, link(is.LogsURL, "Messages from this request"))
		}
		if is.TraceURL != "" {
			links = append(links, link(is.TraceURL, "Request timeline"))
		}
		if len(links) > 0 {
			line(strings.Join(links, " · "))
		}
	}
	if m.More > 0 {
		line("…and " + plural(uint64(m.More), "more problem", "more problems"))
	}
	for _, sec := range m.Sections {
		line("")
		line("<b>" + e(sec.Title) + "</b>")
		for _, l := range sec.Lines {
			line("• " + link(l.URL, l.Lead) + e(l.Text))
		}
	}
	if len(m.Samples) > 0 {
		line("")
		line("<b>Latest matching messages</b>")
		for _, s := range m.Samples {
			line("<code>" + e(sampleWords(s)) + "</code>")
		}
	}
	if m.URL != "" {
		line("")
		line(link(m.URL, "Open in Pyloggle"))
	}
	return strings.TrimRight(b.String(), "\n")
}

// utf16Len is how Telegram measures text.
func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// shrinkSteps returns ever-shorter versions of a message: without the steps, without code,
// with fewer problems and samples, and finally with the headline clipped.
func shrinkSteps(m Message) []Message {
	out := []Message{m}
	cur := m
	step := func(f func(*Message)) {
		next := cur
		next.Issues = append([]MessageIssue(nil), cur.Issues...)
		next.Samples = append([]LogSample(nil), cur.Samples...)
		f(&next)
		out = append(out, next)
		cur = next
	}
	step(func(n *Message) {
		for i := range n.Issues {
			n.Issues[i].Steps = nil
		}
	})
	step(func(n *Message) {
		for i := range n.Issues {
			n.Issues[i].Code = ""
		}
	})
	for _, keep := range []int{3, 1} {
		k := keep
		step(func(n *Message) {
			if len(n.Issues) > k {
				n.More += len(n.Issues) - k
				n.Issues = n.Issues[:k]
			}
			if len(n.Samples) > k {
				n.Samples = n.Samples[:k]
			}
		})
	}
	for _, keep := range []int{3, 1} {
		k := keep
		step(func(n *Message) {
			secs := make([]MessageSection, len(n.Sections))
			for i, s := range n.Sections {
				secs[i] = MessageSection{Title: s.Title, Lines: firstN(s.Lines, k)}
			}
			n.Sections = secs
		})
	}
	step(func(n *Message) {
		n.More += len(n.Issues)
		n.Issues, n.Samples, n.Sections = nil, nil, nil
		n.Headline = clip(n.Headline, 300)
		n.Query = clip(n.Query, 300)
		n.Rule = clip(n.Rule, 100)
	})
	return out
}

// fitTo renders the longest version of m that fits in limit UTF-16 units. ok is false when
// even the shortest does not fit.
func fitTo(m Message, render func(Message) string, limit int) (string, bool) {
	var last string
	for _, v := range shrinkSteps(m) {
		last = render(v)
		if utf16Len(last) <= limit {
			return last, true
		}
	}
	return last, false
}

// telegramText returns the message for Telegram and its parse mode ("HTML" or "").
func telegramText(m Message, useHTML bool) (string, string) {
	if useHTML {
		if s, ok := fitTo(m, renderTelegramHTML, telegramLimit); ok {
			return s, "HTML"
		}
	}
	return telegramPlain(m), ""
}

// telegramPlain fits the plain rendering, cutting it as a last resort.
func telegramPlain(m Message) string {
	s, ok := fitTo(m, renderPlain, telegramLimit)
	if ok {
		return s
	}
	u := utf16.Encode([]rune(s))
	return strings.TrimRight(string(utf16.Decode(u[:telegramLimit-1])), "�") + "…"
}

// --- Slack ---

func slackEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func slackLink(u, text string) string {
	if u == "" {
		return slackEscape(text)
	}
	return "<" + strings.NewReplacer("|", "%7C", ">", "%3E", "<", "%3C").Replace(u) + "|" + slackEscape(text) + ">"
}

func slackCode(s string) string {
	return "`" + slackEscape(strings.ReplaceAll(s, "`", "'")) + "`"
}

// renderSlack lays a message out as Block Kit with a plain fallback text. Slack limits: header
// 150 characters, section text 3000, field 2000, 50 blocks.
func renderSlack(m Message) map[string]any {
	title := m.Title
	if m.Rule != "" {
		title += " · " + m.Rule
	}
	title = clip(title, 150)
	blocks := []map[string]any{
		{"type": "header", "text": map[string]any{"type": "plain_text", "text": title, "emoji": false}},
	}
	if m.Headline != "" {
		blocks = append(blocks, map[string]any{"type": "section", "text": mrkdwn(clip(slackEscape(m.Headline), 3000))})
	}
	fields := []map[string]any{mrkdwn("*Where*\n" + clip(slackEscape(m.Scope), 1900))}
	if m.Window != "" {
		fields = append(fields, mrkdwn("*When*\n"+slackEscape(m.Window)))
	}
	blocks = append(blocks, map[string]any{"type": "section", "fields": fields})
	if m.Query != "" {
		blocks = append(blocks, map[string]any{"type": "section", "text": mrkdwn("*Search*\n" + clip(slackCode(m.Query), 2900))})
	}
	for i, is := range m.Issues {
		var lines []string
		t := fmt.Sprintf("*%d. %s*", i+1, slackLink(is.URL, is.Title))
		if is.Badge != "" {
			t += "  ·  " + slackEscape(is.Badge)
		}
		lines = append(lines, t)
		if is.Counts != "" {
			lines = append(lines, slackEscape(is.Counts))
		}
		if is.Where != "" {
			lines = append(lines, "Happened in "+slackCode(is.Where))
		}
		if is.Code != "" {
			lines = append(lines, slackCode(is.Code))
		}
		if is.Context != "" {
			lines = append(lines, slackEscape(is.Context))
		}
		if len(is.Steps) > 0 {
			lines = append(lines, "Just before:")
			for _, s := range is.Steps {
				lines = append(lines, "• "+slackEscape(s))
			}
		}
		var links []string
		if is.LogsURL != "" {
			links = append(links, slackLink(is.LogsURL, "Messages from this request"))
		}
		if is.TraceURL != "" {
			links = append(links, slackLink(is.TraceURL, "Request timeline"))
		}
		if len(links) > 0 {
			lines = append(lines, strings.Join(links, "  ·  "))
		}
		blocks = append(blocks, map[string]any{"type": "section", "text": mrkdwn(clip(strings.Join(lines, "\n"), 3000))})
	}
	if m.More > 0 {
		blocks = append(blocks, map[string]any{"type": "context", "elements": []map[string]any{mrkdwn("…and " + plural(uint64(m.More), "more problem", "more problems"))}})
	}
	for _, sec := range m.Sections {
		lines := []string{"*" + slackEscape(sec.Title) + "*"}
		for _, l := range sec.Lines {
			lines = append(lines, "• "+slackLink(l.URL, l.Lead)+slackEscape(l.Text))
		}
		blocks = append(blocks, map[string]any{"type": "section", "text": mrkdwn(clip(strings.Join(lines, "\n"), 3000))})
	}
	if len(m.Samples) > 0 {
		var lines []string
		for _, s := range m.Samples {
			lines = append(lines, slackEscape(strings.ReplaceAll(sampleWords(s), "```", "'''")))
		}
		blocks = append(blocks, map[string]any{"type": "section", "text": mrkdwn(clip("*Latest matching messages*\n```"+strings.Join(lines, "\n")+"```", 3000))})
	}
	if m.URL != "" {
		blocks = append(blocks, map[string]any{"type": "actions", "elements": []map[string]any{
			{"type": "button", "text": map[string]any{"type": "plain_text", "text": "Open in Pyloggle"}, "url": m.URL},
		}})
	}
	fallback := title
	if m.Headline != "" {
		fallback += ": " + m.Headline
	}
	return map[string]any{"text": slackEscape(fallback), "blocks": blocks}
}

func mrkdwn(s string) map[string]any { return map[string]any{"type": "mrkdwn", "text": s} }
