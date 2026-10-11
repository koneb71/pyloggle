// Package logquery parses the explorer's search syntax and renders it to parameterized
// ClickHouse SQL.
//
//	service:api env:prod level:(error OR warn) -logger:werkzeug "timed out" @user_id:42
//	http.status:>=500 duration:>250 route:/orders* tags.team:core attrs.order_id:*
//
// Terms are ANDed; the keyword OR between terms starts an alternative group. A field takes one
// value, a quoted value, a prefix/wildcard (*), a comparison (>= > <= <) or a parenthesised OR
// list. Bare words and "quoted phrases" match the message (case-insensitive substring).
// Unknown keys are looked up in attrs (tags.<k> in tags), so custom attributes just work.
package logquery

import (
	"fmt"
	"strings"
	"unicode"
)

// Term is one filter.
type Term struct {
	Field  string   // canonical field name ("" for message text)
	Op     string   // "=", "like", ">", ">=", "<", "<=", "exists"
	Values []string // one or more (OR) values
	Negate bool
	Pos    int // byte offset in the source, for error underlines
}

// Query is a disjunction of conjunctions: Groups are ORed, the Terms inside a group are ANDed.
type Query struct {
	Groups [][]Term
	Raw    string
}

// IsEmpty reports whether the query has no terms.
func (q Query) IsEmpty() bool {
	for _, g := range q.Groups {
		if len(g) > 0 {
			return false
		}
	}
	return true
}

// Error is a parse error with a position.
type Error struct {
	Pos int
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("%s (at %d)", e.Msg, e.Pos) }

type token struct {
	text   string
	quoted bool
	pos    int
}

// tokenize splits on whitespace, keeping quoted strings and parenthesised lists together.
func tokenize(s string) ([]token, error) {
	var out []token
	i := 0
	for i < len(s) {
		c := s[i]
		if unicode.IsSpace(rune(c)) {
			i++
			continue
		}
		start := i
		var b strings.Builder
		quoted := false
		depth := 0
		inQuote := byte(0)
		for i < len(s) {
			c = s[i]
			if inQuote != 0 {
				if c == '\\' && i+1 < len(s) {
					b.WriteByte(s[i+1])
					i += 2
					continue
				}
				if c == inQuote {
					inQuote = 0
					i++
					continue
				}
				b.WriteByte(c)
				i++
				continue
			}
			if c == '"' || c == '\'' {
				inQuote = c
				if b.Len() == 0 && depth == 0 {
					quoted = true
				}
				i++
				continue
			}
			if c == '(' {
				depth++
			} else if c == ')' {
				if depth > 0 {
					depth--
				}
			}
			if depth == 0 && unicode.IsSpace(rune(c)) {
				break
			}
			b.WriteByte(c)
			i++
		}
		if inQuote != 0 {
			return nil, &Error{Pos: start, Msg: "unterminated quote"}
		}
		if depth != 0 {
			return nil, &Error{Pos: start, Msg: "unbalanced parenthesis"}
		}
		out = append(out, token{text: b.String(), quoted: quoted, pos: start})
	}
	return out, nil
}

// Parse parses the query text.
func Parse(s string) (Query, error) {
	q := Query{Raw: s}
	toks, err := tokenize(s)
	if err != nil {
		return q, err
	}
	var group []Term
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if !t.quoted && strings.EqualFold(t.text, "OR") {
			if len(group) == 0 {
				return q, &Error{Pos: t.pos, Msg: "OR needs a term before it"}
			}
			q.Groups = append(q.Groups, group)
			group = nil
			continue
		}
		if !t.quoted && strings.EqualFold(t.text, "AND") {
			continue
		}
		if !t.quoted && t.text == "*" {
			continue
		}
		term, err := parseTerm(t)
		if err != nil {
			return q, err
		}
		group = append(group, term)
	}
	if len(group) > 0 {
		q.Groups = append(q.Groups, group)
	} else if len(q.Groups) > 0 {
		return q, &Error{Pos: len(s), Msg: "OR needs a term after it"}
	}
	return q, nil
}

func parseTerm(t token) (Term, error) {
	text := t.text
	term := Term{Pos: t.pos}
	if !t.quoted && strings.HasPrefix(text, "-") && len(text) > 1 {
		term.Negate = true
		text = text[1:]
	}
	if t.quoted {
		term.Op = "="
		term.Values = []string{text}
		return term, nil
	}
	// field:value — the first ':' splits unless the token starts with a quote.
	colon := strings.IndexByte(text, ':')
	if colon == 0 {
		return term, &Error{Pos: t.pos, Msg: "missing field name before ':'"}
	}
	if colon < 0 {
		// bare word -> message text. Wildcards allowed.
		term.Op = "="
		if strings.ContainsAny(text, "*") {
			term.Op = "like"
		}
		term.Values = []string{text}
		return term, nil
	}
	field := strings.ToLower(strings.TrimSpace(text[:colon]))
	value := text[colon+1:]
	if field == "" {
		return term, &Error{Pos: t.pos, Msg: "missing field name"}
	}
	if value == "" {
		return term, &Error{Pos: t.pos + colon, Msg: "missing value after ':'"}
	}
	term.Field = field
	// Parenthesised OR list: level:(error OR warn)
	if strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")") {
		inner := strings.TrimSpace(value[1 : len(value)-1])
		parts := splitOR(inner)
		if len(parts) == 0 {
			return term, &Error{Pos: t.pos + colon + 1, Msg: "empty value list"}
		}
		term.Op = "="
		for _, p := range parts {
			if strings.ContainsAny(p, "*") {
				term.Op = "like"
			}
			term.Values = append(term.Values, unquote(p))
		}
		return term, nil
	}
	switch {
	case strings.HasPrefix(value, ">="):
		term.Op, term.Values = ">=", []string{value[2:]}
	case strings.HasPrefix(value, "<="):
		term.Op, term.Values = "<=", []string{value[2:]}
	case strings.HasPrefix(value, ">"):
		term.Op, term.Values = ">", []string{value[1:]}
	case strings.HasPrefix(value, "<"):
		term.Op, term.Values = "<", []string{value[1:]}
	case value == "*":
		term.Op = "exists"
	case strings.ContainsAny(value, "*"):
		term.Op, term.Values = "like", []string{unquote(value)}
	default:
		term.Op, term.Values = "=", []string{unquote(value)}
	}
	for _, v := range term.Values {
		if v == "" {
			return term, &Error{Pos: t.pos + colon + 1, Msg: "missing value after comparison"}
		}
	}
	return term, nil
}

// splitOR splits "a OR b OR c" (also accepts commas) outside quotes.
func splitOR(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := byte(0)
	flush := func() {
		v := strings.TrimSpace(cur.String())
		if v != "" {
			out = append(out, v)
		}
		cur.Reset()
	}
	words := strings.Fields(s)
	for _, w := range words {
		if inQuote == 0 && strings.EqualFold(w, "OR") {
			flush()
			continue
		}
		if inQuote == 0 && strings.HasSuffix(w, ",") {
			cur.WriteString(strings.TrimSuffix(w, ","))
			flush()
			continue
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(w)
		for i := 0; i < len(w); i++ {
			if w[i] == '"' || w[i] == '\'' {
				if inQuote == 0 {
					inQuote = w[i]
				} else if inQuote == w[i] {
					inQuote = 0
				}
			}
		}
	}
	flush()
	return out
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
