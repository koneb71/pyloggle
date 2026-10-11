package logquery

import (
	"strings"
	"testing"
)

func mustSQL(t *testing.T, q string) (string, []any) {
	t.Helper()
	parsed, err := Parse(q)
	if err != nil {
		t.Fatalf("parse %q: %v", q, err)
	}
	sql, args, err := parsed.SQL()
	if err != nil {
		t.Fatalf("sql %q: %v", q, err)
	}
	if strings.Count(sql, "?") != len(args) {
		t.Fatalf("%q: %d placeholders vs %d args\n%s\n%v", q, strings.Count(sql, "?"), len(args), sql, args)
	}
	return sql, args
}

func TestParseAndRender(t *testing.T) {
	cases := []struct {
		q       string
		wantSQL []string
		args    int
	}{
		{"service:api env:prod", []string{"service = ?", " AND ", "env = ?"}, 2},
		{`level:error`, []string{"has(?, level)"}, 1},
		{`status:warn`, []string{"has(?, level)"}, 1},
		{`level:(error OR warn)`, []string{"has(?, level)"}, 1},
		{`-logger:werkzeug`, []string{"NOT (logger = ?)"}, 1},
		{`"timed out"`, []string{"positionCaseInsensitiveUTF8(message, ?) > 0"}, 1},
		{`boom`, []string{"positionCaseInsensitiveUTF8(message, ?) > 0"}, 1},
		{`boom*`, []string{"message ILIKE ?"}, 1},
		{`http.status:>=500`, []string{"http_status >= ?"}, 1},
		{`status_code:500`, []string{"http_status = ?"}, 1},
		{`duration:>250`, []string{"duration_ms > ?"}, 1},
		{`route:/orders*`, []string{"http_route LIKE ?"}, 1},
		{`@user_id:42`, []string{"attrs[?] = ?"}, 2},
		{`attrs.order_id:*`, []string{"mapContains(attrs, ?)"}, 1},
		{`tags.team:core`, []string{"tags[?] = ?"}, 2},
		{`custom_key:value`, []string{"attrs[?] = ?"}, 2},
		{`@latency_ms:>100`, []string{"toFloat64OrZero(attrs[?]) > ?"}, 2},
		{`service:api OR service:web`, []string{"(service = ?) OR (service = ?)"}, 2},
		{`service:api level:error OR logger:x`, []string{"(service = ? AND has(?, level)) OR (logger = ?)"}, 3},
		{`service:(api OR web)`, []string{"has(?, service)"}, 1},
		{`@region:(eu OR us)`, []string{"has(?, attrs[?])"}, 2},
		{`message:"disk full"`, []string{"positionCaseInsensitiveUTF8(message, ?) > 0"}, 1},
		{`host:web-1 request:abc trace:def release:1.2.3 issue:xyz`, []string{"hostname = ?", "request_id = ?", "trace_id = ?", "release = ?", "issue_id = ?"}, 5},
		{`* service:api`, []string{"service = ?"}, 1},
		{`service:api AND env:prod`, []string{"service = ?", "env = ?"}, 2},
	}
	for _, c := range cases {
		sql, args := mustSQL(t, c.q)
		for _, w := range c.wantSQL {
			if !strings.Contains(sql, w) {
				t.Errorf("%q: sql %q lacks %q", c.q, sql, w)
			}
		}
		if len(args) != c.args {
			t.Errorf("%q: %d args, want %d (%v)", c.q, len(args), c.args, args)
		}
	}
}

func TestArgsOrderForMapHas(t *testing.T) {
	_, args := mustSQL(t, `@region:(eu OR us)`)
	// has(?, attrs[?]) -> values first, then the key
	if vals, ok := args[0].([]string); !ok || len(vals) != 2 || args[1] != "region" {
		t.Fatalf("args: %#v", args)
	}
	_, args = mustSQL(t, `level:(error OR warning)`)
	vals := args[0].([]string)
	found := map[string]bool{}
	for _, v := range vals {
		found[v] = true
	}
	if !found["error"] || !found["warn"] || !found["warning"] {
		t.Fatalf("level aliases: %v", vals)
	}
}

func TestLikePattern(t *testing.T) {
	if likePattern("a*b%c_d") != `a%b\%c\_d` {
		t.Fatalf("likePattern: %q", likePattern("a*b%c_d"))
	}
}

func TestErrors(t *testing.T) {
	for _, q := range []string{`"unterminated`, `level:(a OR b`, `service:`, `OR service:x`, `service:x OR`, `http.status:abc`, `level:>error`, `:value`} {
		p, err := Parse(q)
		if err == nil {
			_, _, err = p.SQL()
		}
		if err == nil {
			t.Errorf("%q: expected an error", q)
			continue
		}
		if _, ok := err.(*Error); !ok {
			t.Errorf("%q: error must carry a position: %v", q, err)
		}
	}
	if q, err := Parse(""); err != nil || !q.IsEmpty() {
		t.Fatalf("empty query: %v %v", q, err)
	}
	sql, args, _ := Query{}.SQL()
	if sql != "" || len(args) != 0 {
		t.Fatalf("empty SQL: %q %v", sql, args)
	}
}

func TestQuotedValuesAndEscapes(t *testing.T) {
	sql, args := mustSQL(t, `service:"my api" logger:'a b' "it\"s"`)
	if args[0] != "my api" || args[1] != "a b" || args[2] != `it"s` {
		t.Fatalf("args: %#v", args)
	}
	if strings.Count(sql, " AND ") != 2 {
		t.Fatalf("sql: %s", sql)
	}
}

func TestFieldsListed(t *testing.T) {
	f := Fields()
	if len(f) < 15 || f[0] > f[1] {
		t.Fatalf("fields: %v", f)
	}
}
