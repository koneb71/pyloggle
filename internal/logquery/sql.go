package logquery

import (
	"fmt"
	"strconv"
	"strings"

	"pyloggle/internal/model"
)

// column describes how a field maps to SQL.
type column struct {
	expr    string
	kind    string // "string" | "number" | "level" | "text"
	aliases []string
}

var columns = map[string]column{
	"service":     {expr: "service", kind: "string"},
	"env":         {expr: "env", kind: "string", aliases: []string{"environment"}},
	"level":       {expr: "level", kind: "level", aliases: []string{"status", "severity"}},
	"logger":      {expr: "logger", kind: "string"},
	"hostname":    {expr: "hostname", kind: "string", aliases: []string{"host"}},
	"request_id":  {expr: "request_id", kind: "string", aliases: []string{"request"}},
	"trace_id":    {expr: "trace_id", kind: "string", aliases: []string{"trace"}},
	"span_id":     {expr: "span_id", kind: "string", aliases: []string{"span"}},
	"release":     {expr: "release", kind: "string", aliases: []string{"version"}},
	"issue_id":    {expr: "issue_id", kind: "string", aliases: []string{"issue"}},
	"event_id":    {expr: "event_id", kind: "string", aliases: []string{"event"}},
	"source":      {expr: "source", kind: "string"},
	"http.method": {expr: "http_method", kind: "string", aliases: []string{"method"}},
	"http.route":  {expr: "http_route", kind: "string", aliases: []string{"route"}},
	"http.status": {expr: "http_status", kind: "number", aliases: []string{"status_code", "http.status_code"}},
	"http.url":    {expr: "http_url", kind: "string", aliases: []string{"url"}},
	"duration_ms": {expr: "duration_ms", kind: "number", aliases: []string{"duration", "latency", "latency_ms"}},
	"user":        {expr: "user_id_hash", kind: "string", aliases: []string{"user_id", "user.id"}},
	"exception":   {expr: "exception_type", kind: "string", aliases: []string{"exception.type", "error.type"}},
	"message":     {expr: "message", kind: "text", aliases: []string{"msg"}},
}

var aliasIndex = func() map[string]string {
	m := map[string]string{}
	for name, c := range columns {
		m[name] = name
		for _, a := range c.aliases {
			m[a] = name
		}
	}
	return m
}()

// Fields lists the canonical field names (for autocomplete and docs).
func Fields() []string {
	out := make([]string, 0, len(columns)+2)
	for name := range columns {
		out = append(out, name)
	}
	out = append(out, "attrs.<key>", "tags.<key>", "@<key>")
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// resolve maps a user-written field to (expression, kind, mapKey). Unknown fields fall back to
// attrs[key]; "tags.k" and "#k" to tags[key]; "@k" and "attrs.k" to attrs[key].
func resolve(field string) (expr, kind, key string) {
	f := strings.ToLower(strings.TrimSpace(field))
	switch {
	case strings.HasPrefix(f, "@"):
		return "attrs[?]", "string", f[1:]
	case strings.HasPrefix(f, "attrs."):
		return "attrs[?]", "string", f[len("attrs."):]
	case strings.HasPrefix(f, "attr."):
		return "attrs[?]", "string", f[len("attr."):]
	case strings.HasPrefix(f, "tags."):
		return "tags[?]", "string", f[len("tags."):]
	case strings.HasPrefix(f, "tag."):
		return "tags[?]", "string", f[len("tag."):]
	case strings.HasPrefix(f, "#"):
		return "tags[?]", "string", f[1:]
	}
	if name, ok := aliasIndex[f]; ok {
		c := columns[name]
		return c.expr, c.kind, ""
	}
	return "attrs[?]", "string", field
}

// SQL renders the query as a WHERE fragment (without "WHERE") and its positional args.
// An empty query renders to "" with no args.
func (q Query) SQL() (string, []any, error) {
	if q.IsEmpty() {
		return "", nil, nil
	}
	var groups []string
	var args []any
	for _, g := range q.Groups {
		var conds []string
		for _, t := range g {
			c, a, err := termSQL(t)
			if err != nil {
				return "", nil, err
			}
			conds = append(conds, c)
			args = append(args, a...)
		}
		groups = append(groups, "("+strings.Join(conds, " AND ")+")")
	}
	if len(groups) == 1 {
		return strings.TrimSuffix(strings.TrimPrefix(groups[0], "("), ")"), args, nil
	}
	return strings.Join(groups, " OR "), args, nil
}

func termSQL(t Term) (string, []any, error) {
	var cond string
	var args []any

	if t.Field == "" {
		// message text
		var parts []string
		for _, v := range t.Values {
			if t.Op == "like" {
				parts = append(parts, "message ILIKE ?")
				args = append(args, likePattern(v))
			} else {
				parts = append(parts, "positionCaseInsensitiveUTF8(message, ?) > 0")
				args = append(args, v)
			}
		}
		cond = strings.Join(parts, " OR ")
		if len(parts) > 1 {
			cond = "(" + cond + ")"
		}
		return negate(cond, t.Negate), args, nil
	}

	expr, kind, key := resolve(t.Field)
	keyArgs := []any{}
	if key != "" {
		keyArgs = []any{key}
	}

	switch t.Op {
	case "exists":
		if key != "" {
			col := "attrs"
			if strings.HasPrefix(expr, "tags") {
				col = "tags"
			}
			return negate("mapContains("+col+", ?)", t.Negate), []any{key}, nil
		}
		return negate(expr+" != ''", t.Negate), nil, nil
	case ">", ">=", "<", "<=":
		if kind == "level" {
			return "", nil, &Error{Pos: t.Pos, Msg: "comparisons are not supported for level"}
		}
		v := t.Values[0]
		if kind == "number" {
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return "", nil, &Error{Pos: t.Pos, Msg: fmt.Sprintf("%s expects a number, got %q", t.Field, v)}
			}
			args = append(append(args, keyArgs...), n)
			return negate(expr+" "+t.Op+" ?", t.Negate), args, nil
		}
		if key != "" {
			// map values are strings; compare numerically when possible
			if n, err := strconv.ParseFloat(v, 64); err == nil {
				args = append(append(args, keyArgs...), n)
				return negate("toFloat64OrZero("+expr+") "+t.Op+" ?", t.Negate), args, nil
			}
		}
		args = append(append(args, keyArgs...), v)
		return negate(expr+" "+t.Op+" ?", t.Negate), args, nil
	}

	// "=" / "like" with one or more values
	if kind == "level" {
		var all []string
		for _, v := range t.Values {
			all = append(all, model.LevelAliases(v)...)
		}
		return negate("has(?, level)", t.Negate), []any{all}, nil
	}
	if kind == "text" {
		var parts []string
		for _, v := range t.Values {
			if t.Op == "like" || strings.ContainsAny(v, "*") {
				parts = append(parts, expr+" ILIKE ?")
				args = append(args, likePattern(v))
			} else {
				parts = append(parts, "positionCaseInsensitiveUTF8("+expr+", ?) > 0")
				args = append(args, v)
			}
		}
		cond = strings.Join(parts, " OR ")
		if len(parts) > 1 {
			cond = "(" + cond + ")"
		}
		return negate(cond, t.Negate), args, nil
	}
	if kind == "number" {
		var parts []string
		for _, v := range t.Values {
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return "", nil, &Error{Pos: t.Pos, Msg: fmt.Sprintf("%s expects a number, got %q", t.Field, v)}
			}
			parts = append(parts, expr+" = ?")
			args = append(args, n)
		}
		cond = strings.Join(parts, " OR ")
		if len(parts) > 1 {
			cond = "(" + cond + ")"
		}
		return negate(cond, t.Negate), args, nil
	}
	// string
	if t.Op == "like" {
		var parts []string
		for _, v := range t.Values {
			parts = append(parts, expr+" LIKE ?")
			args = append(append(args, keyArgs...), likePattern(v))
		}
		cond = strings.Join(parts, " OR ")
		if len(parts) > 1 {
			cond = "(" + cond + ")"
		}
		return negate(cond, t.Negate), args, nil
	}
	if len(t.Values) == 1 {
		args = append(append(args, keyArgs...), t.Values[0])
		return negate(expr+" = ?", t.Negate), args, nil
	}
	args = append(append(args, keyArgs...), t.Values)
	return negate("has(?, "+expr+")", t.Negate), reorderHas(args, len(keyArgs)), nil
}

// reorderHas puts the values array before the map key so placeholders line up with
// "has(?, attrs[?])" (values first, then key).
func reorderHas(args []any, keyArgs int) []any {
	if keyArgs == 0 {
		return args
	}
	vals := args[len(args)-1]
	return append([]any{vals}, args[:len(args)-1]...)
}

func negate(cond string, neg bool) string {
	if neg {
		return "NOT (" + cond + ")"
	}
	return cond
}

// likePattern turns a wildcard value into a LIKE pattern, escaping % and _.
func likePattern(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch r {
		case '*':
			b.WriteByte('%')
		case '%', '_', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
