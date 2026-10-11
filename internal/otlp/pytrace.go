package otlp

import (
	"regexp"
	"strconv"
	"strings"

	"pyloggle/internal/model"
)

// ParsePythonTraceback turns the text Python's traceback module prints (what OTEL puts into
// exception.stacktrace) back into structured frames and a chain of causes.
//
// Sections are separated by "The above exception was the direct cause of the following
// exception:" (cause) or "During handling of the above exception, another exception occurred:"
// (context). Python prints the root cause first; the last section is the exception that was
// actually raised, which becomes the top-level exception, and earlier sections become the chain
// (nearest first).
func ParsePythonTraceback(s string) (excType, excMsg string, frames []model.Frame, chain []model.ChainedException, ok bool) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.Contains(s, "Traceback (most recent call last)") && !strings.Contains(s, "  File \"") {
		return "", "", nil, nil, false
	}
	type section struct {
		frames  []model.Frame
		typ     string
		msg     string
		isCause bool // relation to the NEXT section
	}
	var sections []section
	cur := section{}
	lines := strings.Split(s, "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "The above exception was the direct cause"):
			cur.isCause = true
			sections = append(sections, cur)
			cur = section{}
			continue
		case strings.HasPrefix(trim, "During handling of the above exception"):
			cur.isCause = false
			sections = append(sections, cur)
			cur = section{}
			continue
		case strings.HasPrefix(trim, "Traceback (most recent call last)"):
			continue
		}
		if m := reFrame.FindStringSubmatch(line); m != nil {
			fr := model.Frame{Filename: m[1], Function: m[3]}
			if n, err := strconv.Atoi(m[2]); err == nil {
				fr.Lineno = uint32(n)
			}
			// The source line follows, indented; caret lines (3.11+) are skipped.
			if i+1 < len(lines) {
				next := lines[i+1]
				nt := strings.TrimSpace(next)
				if nt != "" && !reFrame.MatchString(next) && !isCaretLine(nt) && !looksLikeExceptionLine(next) {
					fr.Context = nt
					i++
					if i+1 < len(lines) && isCaretLine(strings.TrimSpace(lines[i+1])) {
						i++
					}
				}
			}
			cur.frames = append(cur.frames, fr)
			continue
		}
		if looksLikeExceptionLine(line) && len(cur.frames) > 0 && cur.typ == "" {
			typ, msg := splitExceptionLine(trim)
			cur.typ, cur.msg = typ, msg
			continue
		}
		// Continuation of a multi-line message.
		if cur.typ != "" && trim != "" {
			cur.msg += "\n" + trim
		}
	}
	if len(cur.frames) > 0 || cur.typ != "" {
		sections = append(sections, cur)
	}
	if len(sections) == 0 {
		return "", "", nil, nil, false
	}
	last := sections[len(sections)-1]
	excType, excMsg, frames = last.typ, last.msg, last.frames
	for i := len(sections) - 2; i >= 0; i-- {
		sec := sections[i]
		rel := "context"
		if sec.isCause {
			rel = "cause"
		}
		chain = append(chain, model.ChainedException{Type: sec.typ, Message: sec.msg, Frames: sec.frames, IsCause: sec.isCause, Relation: rel})
	}
	return excType, excMsg, frames, chain, true
}

var reFrame = regexp.MustCompile(`^\s*File "(.+?)", line (\d+), in (.+)$`)
var reExcLine = regexp.MustCompile(`^[A-Za-z_][\w.]*(?:: ?.*)?$`)

func isCaretLine(trim string) bool {
	if trim == "" {
		return false
	}
	for _, r := range trim {
		if r != '^' && r != '~' && r != ' ' {
			return false
		}
	}
	return true
}

// looksLikeExceptionLine matches "ValueError: msg" / "ValueError" at column 0.
func looksLikeExceptionLine(line string) bool {
	if line == "" || line[0] == ' ' || line[0] == '\t' {
		return false
	}
	return reExcLine.MatchString(line)
}

func splitExceptionLine(trim string) (string, string) {
	if i := strings.Index(trim, ": "); i > 0 {
		return trim[:i], trim[i+2:]
	}
	if strings.HasSuffix(trim, ":") {
		return strings.TrimSuffix(trim, ":"), ""
	}
	return trim, ""
}

// SplitTypeModule separates "requests.exceptions.ConnectionError" into module and type.
func SplitTypeModule(full string) (module, typ string) {
	if i := strings.LastIndex(full, "."); i > 0 {
		return full[:i], full[i+1:]
	}
	return "", full
}
