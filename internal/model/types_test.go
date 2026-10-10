package model

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTimestampUnmarshalForms(t *testing.T) {
	want := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		`"2025-01-01T00:00:00Z"`:      want,
		`"2025-01-01T00:00:00"`:       want, // v0.1 logging handler: no zone
		`"2025-01-01T01:00:00+01:00"`: want,
		`"2025-01-01T00:00:00.250Z"`:  want.Add(250 * time.Millisecond),
		`"2025-01-01 00:00:00"`:       want,
		`1735689600`:                  want,
		`1735689600000`:               want,
		`1735689600.5`:                want.Add(500 * time.Millisecond),
		`1735689600000000000`:         want,
		`""`:                          {},
		`null`:                        {},
	}
	for in, exp := range cases {
		var ts Timestamp
		if err := json.Unmarshal([]byte(in), &ts); err != nil {
			t.Errorf("%s: %v", in, err)
			continue
		}
		if !ts.Equal(exp) {
			t.Errorf("%s: got %v want %v", in, ts.Time, exp)
		}
	}
	var bad Timestamp
	if err := json.Unmarshal([]byte(`"yesterday-ish"`), &bad); err == nil {
		t.Fatalf("garbage must fail")
	}
	out, _ := json.Marshal(At(want.Add(5 * time.Millisecond)))
	if string(out) != `"2025-01-01T00:00:00.005Z"` {
		t.Fatalf("marshal: %s", out)
	}
}

func TestAttrsUnmarshalStringifies(t *testing.T) {
	var a Attrs
	in := `{"s":"x","b":true,"n":12,"f":1.5,"nil":null,"o":{"k": [1, 2]},"arr":[1,"a"]}`
	if err := json.Unmarshal([]byte(in), &a); err != nil {
		t.Fatal(err)
	}
	want := Attrs{"s": "x", "b": "true", "n": "12", "f": "1.5", "o": `{"k":[1,2]}`, "arr": `[1,"a"]`}
	if len(a) != len(want) {
		t.Fatalf("got %v want %v", a, want)
	}
	for k, v := range want {
		if a[k] != v {
			t.Errorf("%s: got %q want %q", k, a[k], v)
		}
	}
	// A non-object attrs value must not reject the event.
	var b Attrs
	if err := json.Unmarshal([]byte(`"oops"`), &b); err != nil || b != nil {
		t.Fatalf("non-object attrs: %v %v", err, b)
	}
}

func TestJSONText(t *testing.T) {
	var j JSONText
	_ = json.Unmarshal([]byte(`{"a": 1, "b": "x"}`), &j)
	if j != `{"a":1,"b":"x"}` {
		t.Fatalf("object: %q", j)
	}
	_ = json.Unmarshal([]byte(`"plain"`), &j)
	if j != "plain" {
		t.Fatalf("string: %q", j)
	}
	_ = json.Unmarshal([]byte(`null`), &j)
	if j != "" {
		t.Fatalf("null: %q", j)
	}
}

func TestNormalizeLevel(t *testing.T) {
	cases := map[string]string{
		"WARNING": "warn", "warn": "warn", "critical": "fatal", "CRITICAL": "fatal", "fatal": "fatal",
		"error": "error", "err": "error", "info": "info", "": "info", "debug": "debug", "trace": "debug",
		"notice": "info", "audit": "audit",
	}
	for in, want := range cases {
		if got := NormalizeLevel(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
	if !IsErrorLevel("critical") || !IsErrorLevel("error") || IsErrorLevel("warning") {
		t.Fatal("IsErrorLevel")
	}
}
