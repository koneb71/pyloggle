// Package issue turns error events into occurrences and groups them into issues.
//
// Grouping (fingerprint v2) deliberately ignores line numbers: an issue is identified by the
// exception type (plus module for non-builtins), the innermost in-app frames as
// module-or-path + function, and the types of chained causes. Shifting code around therefore
// keeps an issue stable, while a different code path or a different root cause forks it.
package issue

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"time"

	"github.com/koneb71/pyloggle/internal/model"
)

// FingerprintVersion is stored on every occurrence so a future change can be told apart.
const FingerprintVersion uint8 = 2

// ErrorOccurrence is one stored error event (one row in error_occurrences).
type ErrorOccurrence struct {
	TS        time.Time
	EventID   string
	IssueID   string
	Service   string
	Env       string
	ProjectID string

	Level   string
	Logger  string
	Message string

	ExceptionType      string
	ExceptionModule    string
	ExceptionMessage   string
	StackFingerprint   string
	FingerprintVersion uint8
	Title              string
	Culprit            string

	StackFrames []StackFrame // legacy tuple column (filename, lineno, function, context, in_app)
	Frames      []model.Frame
	Chain       []model.ChainedException
	Notes       []string
	Breadcrumbs []model.Breadcrumb

	TraceID   string
	SpanID    string
	RequestID string

	HTTP    model.HTTPInfo
	User    model.UserInfo
	Runtime model.RuntimeInfo
	SDK     model.SDKInfo

	Release  string
	Hostname string
	Source   string

	MechanismType    string
	MechanismHandled bool

	Tags  map[string]string
	Attrs map[string]string
	Extra string
}

// StackFrame mirrors the legacy ClickHouse tuple.
type StackFrame struct {
	Filename string
	Lineno   uint32
	Function string
	Context  string
	IsInApp  uint8
}

// FromEvent builds the occurrence for an error event: resolves in-app flags, computes the
// fingerprint, issue id, culprit and title.
func FromEvent(ev model.Event) ErrorOccurrence {
	occ := ErrorOccurrence{
		TS:                 ev.Timestamp.UTC(),
		EventID:            ev.EventID,
		Service:            ev.Service,
		Env:                ev.Env,
		ProjectID:          ev.ProjectID,
		Level:              ev.Level,
		Logger:             ev.Logger,
		Message:            ev.Message,
		FingerprintVersion: FingerprintVersion,
		TraceID:            ev.TraceID,
		SpanID:             ev.SpanID,
		RequestID:          ev.RequestID,
		Release:            ev.Release,
		Hostname:           ev.Hostname,
		Source:             ev.Source,
		MechanismHandled:   ev.Mechanism.IsHandled(),
		Tags:               ev.Tags,
		Attrs:              ev.Attrs,
		Extra:              string(ev.Extra),
		Breadcrumbs:        ev.Breadcrumbs,
	}
	if occ.Level == "" {
		occ.Level = model.LevelError
	}
	if ev.Mechanism != nil {
		occ.MechanismType = ev.Mechanism.Type
	}
	if ev.HTTP != nil {
		occ.HTTP = *ev.HTTP
	}
	if ev.User != nil {
		occ.User = *ev.User
	}
	if ev.Runtime != nil {
		occ.Runtime = *ev.Runtime
	}
	if ev.SDK != nil {
		occ.SDK = *ev.SDK
	}
	if ev.Exception != nil {
		occ.ExceptionType = ev.Exception.Type
		occ.ExceptionModule = ev.Exception.Module
		occ.ExceptionMessage = ev.Exception.Message
		occ.Frames = ResolveInApp(ev.Exception.Frames)
		occ.Chain = make([]model.ChainedException, len(ev.Exception.Chain))
		for i, c := range ev.Exception.Chain {
			c.Frames = ResolveInApp(c.Frames)
			occ.Chain[i] = c
		}
		occ.Notes = ev.Exception.Notes
		occ.StackFrames = legacyFrames(occ.Frames)
	}
	occ.StackFingerprint = Fingerprint(ev, occ.Frames)
	occ.IssueID = IssueID(occ.ProjectID, occ.Service, occ.Env, occ.StackFingerprint)
	occ.Culprit = Culprit(occ.Frames)
	occ.Title = Title(ev)
	return occ
}

// ResolveInApp returns a copy of frames with every nil in_app resolved through the path heuristic.
func ResolveInApp(frames []model.Frame) []model.Frame {
	out := make([]model.Frame, len(frames))
	for i, f := range frames {
		if f.InApp == nil {
			v := IsInAppFilename(f.Filename)
			f.InApp = &v
		}
		out[i] = f
	}
	return out
}

func legacyFrames(frames []model.Frame) []StackFrame {
	out := make([]StackFrame, 0, len(frames))
	for _, f := range frames {
		var inApp uint8
		if f.InApp != nil && *f.InApp {
			inApp = 1
		}
		out = append(out, StackFrame{Filename: f.Filename, Lineno: f.Lineno, Function: f.Function, Context: f.Context, IsInApp: inApp})
	}
	return out
}

// IsInAppFilename is the server-side heuristic for frames whose producer did not say.
func IsInAppFilename(filename string) bool {
	if filename == "" {
		return false
	}
	fn := strings.ReplaceAll(filename, "\\", "/")
	l := strings.ToLower(fn)
	if strings.HasPrefix(l, "<frozen") || strings.HasPrefix(l, "<string>") || strings.HasPrefix(l, "<stdin>") {
		return false
	}
	for _, marker := range []string{"/site-packages/", "/dist-packages/", "/.venv/", "/venv/", "/usr/local/lib/python", "/usr/lib/python", "importlib/_bootstrap", "/lib/python3", "/node_modules/", "/go/pkg/mod/"} {
		if strings.Contains(l, marker) {
			return false
		}
	}
	return true
}

var builtinModules = map[string]bool{"": true, "builtins": true, "__main__": true, "exceptions": true}

// Fingerprint computes the grouping key. frames must already have in_app resolved.
func Fingerprint(ev model.Event, frames []model.Frame) string {
	if len(ev.Fingerprint) > 0 {
		parts := make([]string, 0, len(ev.Fingerprint))
		for _, p := range ev.Fingerprint {
			p = strings.TrimSpace(p)
			if p == "{{ default }}" || p == "{{default}}" {
				p = defaultFingerprint(ev, frames)
			}
			parts = append(parts, p)
		}
		return hash("custom\n" + strings.Join(parts, "\n"))
	}
	return hash(defaultFingerprint(ev, frames))
}

func defaultFingerprint(ev model.Event, frames []model.Frame) string {
	if ev.Exception == nil {
		return "message\n" + ev.Logger + "\n" + NormalizeMessage(ev.Message)
	}
	var b strings.Builder
	b.WriteString("exception\n")
	b.WriteString(exceptionKey(ev.Exception.Type, ev.Exception.Module))
	b.WriteByte('\n')
	n := 0
	for i := len(frames) - 1; i >= 0 && n < 6; i-- {
		f := frames[i]
		if f.InApp == nil || !*f.InApp {
			continue
		}
		b.WriteString(frameKey(f))
		b.WriteByte('\n')
		n++
	}
	if n == 0 {
		// Library-only traceback: group by the innermost frames of any kind.
		for i := len(frames) - 1; i >= 0 && n < 3; i-- {
			b.WriteString(frameKey(frames[i]))
			b.WriteByte('\n')
			n++
		}
	}
	for i, c := range ev.Exception.Chain {
		if i >= 3 {
			break
		}
		b.WriteString("cause:" + exceptionKey(c.Type, c.Module))
		b.WriteByte('\n')
	}
	return b.String()
}

func exceptionKey(typ, module string) string {
	if builtinModules[module] {
		return typ
	}
	return module + "." + typ
}

func frameKey(f model.Frame) string {
	loc := f.Module
	if loc == "" {
		loc = lastSegments(f.Filename, 2)
	}
	return loc + ":" + f.Function
}

// lastSegments keeps the last n path segments ("/srv/app/pkg/views.py" -> "pkg/views.py").
func lastSegments(path string, n int) string {
	p := strings.ReplaceAll(path, "\\", "/")
	parts := strings.Split(p, "/")
	if len(parts) > n {
		parts = parts[len(parts)-n:]
	}
	return strings.Join(parts, "/")
}

// IssueID scopes a fingerprint to project/service/env.
func IssueID(projectID, service, env, fingerprint string) string {
	return hash(projectID + "|" + service + "|" + env + "|" + fingerprint)
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Culprit names the innermost in-app frame ("app.views in checkout"), else the innermost frame.
func Culprit(frames []model.Frame) string {
	var pick *model.Frame
	for i := len(frames) - 1; i >= 0; i-- {
		if frames[i].InApp != nil && *frames[i].InApp {
			pick = &frames[i]
			break
		}
	}
	if pick == nil && len(frames) > 0 {
		pick = &frames[len(frames)-1]
	}
	if pick == nil {
		return ""
	}
	loc := pick.Module
	if loc == "" {
		loc = lastSegments(pick.Filename, 2)
	}
	if pick.Function == "" {
		return loc
	}
	return loc + " in " + pick.Function
}

// Title renders "Type: message" (one line, capped) or the message template for log-only issues.
func Title(ev model.Event) string {
	if ev.Exception != nil {
		typ := strings.TrimSpace(ev.Exception.Type)
		if typ == "" {
			typ = "Exception"
		}
		msg := oneLine(ev.Exception.Message)
		if msg == "" {
			return cut(typ, 200)
		}
		return cut(typ+": "+msg, 200)
	}
	t := NormalizeMessage(ev.Message)
	if t == "" {
		t = ev.Logger
	}
	if t == "" {
		t = "Error"
	}
	return cut(t, 200)
}

var (
	reUUID   = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	reQuoted = regexp.MustCompile(`"[^"]*"|'[^']*'`)
	reHex    = regexp.MustCompile(`\b[0-9a-fA-F]{8,}\b`)
	reNum    = regexp.MustCompile(`\d+(\.\d+)?`)
	reSpace  = regexp.MustCompile(`\s+`)
)

// NormalizeMessage turns a log message into its template: uuids, quoted strings, long hex ids
// and numbers become placeholders so "user 42 not found" and "user 43 not found" group together.
// The ClickHouse "patterns" query applies the same substitutions in the same order.
func NormalizeMessage(s string) string {
	s = reUUID.ReplaceAllString(s, "<uuid>")
	s = reQuoted.ReplaceAllString(s, "<s>")
	s = reHex.ReplaceAllString(s, "<hex>")
	s = reNum.ReplaceAllString(s, "<n>")
	s = strings.TrimSpace(reSpace.ReplaceAllString(s, " "))
	return cut(s, 200)
}

func oneLine(s string) string {
	return strings.TrimSpace(reSpace.ReplaceAllString(s, " "))
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// avoid splitting a multi-byte rune
	for n > 0 && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "…"
}
