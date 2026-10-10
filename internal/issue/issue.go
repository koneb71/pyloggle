package issue

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"pyloggle/internal/model"
)

type ErrorOccurrence struct {
	TS        time.Time
	IssueID   string
	Service   string
	Env       string
	ProjectID string

	ExceptionType    string
	ExceptionMessage string
	StackFingerprint string
	StackFrames      []StackFrame

	TraceID   string
	SpanID    string
	RequestID string

	HTTPMethod     string
	HTTPRoute      string
	HTTPStatus     uint16
	HTTPUserAgent  string
	HTTPRemoteAddr string
	HTTPServerAddr string

	UserIDHash string

	Attrs map[string]string
	Title string
}

type StackFrame struct {
	Filename string
	Lineno   uint32
	Function string
	Context  string
	IsInApp  uint8
}

func FromEvent(ev model.Event) ErrorOccurrence {
	occ := ErrorOccurrence{
		TS:        ev.Timestamp.UTC(),
		Service:   ev.Service,
		Env:       ev.Env,
		ProjectID: ev.ProjectID,
		TraceID:   ev.TraceID,
		SpanID:    ev.SpanID,
		RequestID: ev.RequestID,
		Attrs:     ev.Attrs,
	}

	if ev.HTTP != nil {
		occ.HTTPMethod = ev.HTTP.Method
		occ.HTTPRoute = ev.HTTP.Route
		occ.HTTPStatus = ev.HTTP.Status
		occ.HTTPUserAgent = ev.HTTP.UserAgent
		occ.HTTPRemoteAddr = ev.HTTP.RemoteAddr
		occ.HTTPServerAddr = ev.HTTP.ServerAddr
	}
	if ev.User != nil {
		occ.UserIDHash = ev.User.IDHash
	}
	if ev.Exception != nil {
		occ.ExceptionType = ev.Exception.Type
		occ.ExceptionMessage = ev.Exception.Message
		occ.StackFrames = make([]StackFrame, 0, len(ev.Exception.Frames))
		for _, f := range ev.Exception.Frames {
			var inApp uint8
			// Prefer explicit in_app from SDK; otherwise fall back to server-side heuristics
			// so grouping remains stable and the "your code" frame is highlighted.
			if f.InApp || isInAppFilename(f.Filename) {
				inApp = 1
			}
			occ.StackFrames = append(occ.StackFrames, StackFrame{
				Filename: f.Filename,
				Lineno:   f.Lineno,
				Function: f.Function,
				Context:  f.Context,
				IsInApp:  inApp,
			})
		}
	}

	occ.StackFingerprint = stackFingerprint(occ.ExceptionType, occ.StackFrames)
	occ.IssueID = issueID(occ.Service, occ.Env, occ.ProjectID, occ.StackFingerprint)
	occ.Title = issueTitle(occ.ExceptionType, occ.StackFrames)
	return occ
}

func isInAppFilename(filename string) bool {
	if filename == "" {
		return false
	}
	fn := strings.ReplaceAll(filename, "\\", "/")
	l := strings.ToLower(fn)

	// Exclude common library / stdlib locations.
	if strings.Contains(l, "/site-packages/") || strings.Contains(l, "/dist-packages/") {
		return false
	}
	if strings.Contains(l, "/.venv/") || strings.Contains(l, "/venv/") {
		return false
	}
	if strings.Contains(l, "/usr/local/lib/python") || strings.Contains(l, "/usr/lib/python") {
		return false
	}
	if strings.Contains(l, "/lib/python") && strings.Contains(l, "/python") {
		return false
	}

	// Assume everything else is first-party.
	return true
}

func stackFingerprint(exceptionType string, frames []StackFrame) string {
	h := sha256.New()
	_, _ = h.Write([]byte(exceptionType))
	_, _ = h.Write([]byte{'\n'})

	// Use top N in-app frames to stabilize grouping.
	const topN = 6
	n := 0
	for _, fr := range frames {
		if fr.IsInApp != 1 {
			continue
		}
		_, _ = h.Write([]byte(fr.Filename))
		_, _ = h.Write([]byte{':'})
		_, _ = h.Write([]byte(u32(fr.Lineno)))
		_, _ = h.Write([]byte{':'})
		_, _ = h.Write([]byte(fr.Function))
		_, _ = h.Write([]byte{'\n'})
		n++
		if n >= topN {
			break
		}
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum)
}

func issueID(service, env, projectID, stackFingerprint string) string {
	sum := sha256.Sum256([]byte(projectID + "|" + service + "|" + env + "|" + stackFingerprint))
	return hex.EncodeToString(sum[:])
}

func issueTitle(exceptionType string, frames []StackFrame) string {
	top := firstInApp(frames)
	if top == nil {
		if exceptionType == "" {
			return "UnknownError"
		}
		return exceptionType
	}
	fn := shortFile(top.Filename)
	return strings.TrimSpace(exceptionType) + " in " + top.Function + " (" + fn + ":" + u32(top.Lineno) + ")"
}

func firstInApp(frames []StackFrame) *StackFrame {
	for i := range frames {
		if frames[i].IsInApp == 1 {
			return &frames[i]
		}
	}
	if len(frames) > 0 {
		return &frames[0]
	}
	return nil
}

func shortFile(path string) string {
	// keep it simple: last segment
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}

func u32(n uint32) string {
	if n == 0 {
		return "0"
	}
	var b [32]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
