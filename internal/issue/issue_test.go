package issue

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/koneb71/pyloggle/internal/model"
)

func b(v bool) *bool { return &v }

func frames(lines ...uint32) []model.Frame {
	fr := []model.Frame{
		{Filename: "/usr/lib/python3.12/site-packages/flask/app.py", Function: "full_dispatch_request", Lineno: 880},
		{Filename: "/srv/app/views.py", Module: "app.views", Function: "checkout", Lineno: lines[0], InApp: b(true)},
		{Filename: "/srv/app/payments.py", Module: "app.payments", Function: "charge", Lineno: lines[1], InApp: b(true)},
	}
	return ResolveInApp(fr)
}

func exceptionEvent(lines ...uint32) model.Event {
	return model.Event{
		ProjectID: "p", Service: "api", Env: "prod", Level: "error",
		Exception: &model.Exception{Type: "ValueError", Module: "builtins", Message: "bad amount 42", Frames: frames(lines...)},
	}
}

func TestFingerprintIgnoresLineNumbers(t *testing.T) {
	a := FromEvent(exceptionEvent(10, 20))
	b2 := FromEvent(exceptionEvent(11, 27))
	if a.StackFingerprint != b2.StackFingerprint || a.IssueID != b2.IssueID {
		t.Fatalf("line shifts must not fork issues")
	}
	c := exceptionEvent(10, 20)
	c.Exception.Frames[2].Function = "refund"
	if FromEvent(c).StackFingerprint == a.StackFingerprint {
		t.Fatalf("a different function must fork the issue")
	}
	d := exceptionEvent(10, 20)
	d.Exception.Message = "bad amount 99"
	if FromEvent(d).StackFingerprint != a.StackFingerprint {
		t.Fatalf("the message must not affect grouping")
	}
	e := exceptionEvent(10, 20)
	e.Exception.Type = "TypeError"
	if FromEvent(e).StackFingerprint == a.StackFingerprint {
		t.Fatalf("a different exception type must fork the issue")
	}
	if a.FingerprintVersion != 2 {
		t.Fatalf("fingerprint version")
	}
}

func TestFingerprintModuleAndChain(t *testing.T) {
	a := exceptionEvent(1, 2)
	b2 := exceptionEvent(1, 2)
	b2.Exception.Module = "requests.exceptions"
	if FromEvent(a).StackFingerprint == FromEvent(b2).StackFingerprint {
		t.Fatalf("library exception modules distinguish issues")
	}
	c := exceptionEvent(1, 2)
	c.Exception.Module = "__main__"
	if FromEvent(a).StackFingerprint != FromEvent(c).StackFingerprint {
		t.Fatalf("builtins/__main__/empty module hash alike (v0.1 compatibility)")
	}
	d := exceptionEvent(1, 2)
	d.Exception.Chain = []model.ChainedException{{Type: "KeyError", IsCause: true}}
	if FromEvent(a).StackFingerprint == FromEvent(d).StackFingerprint {
		t.Fatalf("a different root cause forks the issue")
	}
}

func TestCustomFingerprint(t *testing.T) {
	a := exceptionEvent(1, 2)
	a.Fingerprint = []string{"payments", "timeout"}
	b2 := exceptionEvent(5, 6)
	b2.Fingerprint = []string{"payments", "timeout"}
	b2.Exception.Type = "TimeoutError"
	if FromEvent(a).StackFingerprint != FromEvent(b2).StackFingerprint {
		t.Fatalf("custom fingerprints group regardless of the stack")
	}
	c := exceptionEvent(1, 2)
	c.Fingerprint = []string{"{{ default }}", "tenant-7"}
	if FromEvent(c).StackFingerprint == FromEvent(exceptionEvent(1, 2)).StackFingerprint {
		t.Fatalf("{{ default }} plus an extra part must differ from the default")
	}
}

func TestMessageOnlyIssues(t *testing.T) {
	ev := func(msg string) model.Event {
		return model.Event{ProjectID: "p", Service: "api", Env: "prod", Level: "error", Logger: "app.db", Message: msg}
	}
	a := FromEvent(ev("connection to db-01 lost after 3000ms (attempt 2)"))
	b2 := FromEvent(ev("connection to db-02 lost after 12ms (attempt 7)"))
	if a.StackFingerprint != b2.StackFingerprint {
		t.Fatalf("numbers must be templated: %q vs %q", a.Title, b2.Title)
	}
	c := FromEvent(ev("payment declined for order abc"))
	if c.StackFingerprint == a.StackFingerprint {
		t.Fatalf("different templates are different issues")
	}
	if a.Title != "connection to db-<n> lost after <n>ms (attempt <n>)" {
		t.Fatalf("title: %q", a.Title)
	}
	if a.Culprit != "" || a.ExceptionType != "" {
		t.Fatalf("log-only issues have no culprit/exception")
	}
}

func TestNormalizeMessage(t *testing.T) {
	cases := map[string]string{
		`user 42 not found`: `user <n> not found`,
		`order 3fa85f64-5717-4562-b3fc-2c963f66afa6 failed`: `order <uuid> failed`,
		`token deadbeefcafe expired`:                        `token <hex> expired`,
		`key 'x' missing in "config"`:                       `key <s> missing in <s>`,
		"  spaced   out\n lines ":                           `spaced out lines`,
		`version 1.2.3 ok`:                                  `version <n>.<n> ok`,
	}
	for in, want := range cases {
		if got := NormalizeMessage(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestCulpritAndTitle(t *testing.T) {
	occ := FromEvent(exceptionEvent(10, 20))
	if occ.Culprit != "app.payments in charge" {
		t.Fatalf("culprit must be the innermost in-app frame, got %q", occ.Culprit)
	}
	if occ.Title != "ValueError: bad amount 42" {
		t.Fatalf("title: %q", occ.Title)
	}
	// Explicit in_app=false from the SDK is honored even for app-looking paths.
	ev := exceptionEvent(10, 20)
	ev.Exception.Frames[2].InApp = b(false)
	if got := FromEvent(ev).Culprit; got != "app.views in checkout" {
		t.Fatalf("explicit in_app=false ignored: %q", got)
	}
	// No in-app frames at all: innermost library frame.
	ev2 := model.Event{Exception: &model.Exception{Type: "OSError", Frames: ResolveInApp([]model.Frame{
		{Filename: "/x/site-packages/a/b.py", Function: "f"},
		{Filename: "/x/site-packages/a/c.py", Function: "g"},
	})}}
	if got := FromEvent(ev2).Culprit; got != "a/c.py in g" {
		t.Fatalf("library culprit: %q", got)
	}
	if len(occ.StackFrames) != 3 || occ.StackFrames[2].IsInApp != 1 || occ.StackFrames[0].IsInApp != 0 {
		t.Fatalf("legacy frames: %+v", occ.StackFrames)
	}
}

func TestInAppHeuristic(t *testing.T) {
	for path, want := range map[string]bool{
		"/srv/app/main.py":                             true,
		"/usr/lib/python3.12/json/decoder.py":          false,
		"/home/u/.venv/lib/python3.12/site-packages/x": false,
		"<frozen importlib._bootstrap>":                false,
		"C:\\project\\venv\\Lib\\site-packages\\a.py":  false,
		"": false,
	} {
		if got := IsInAppFilename(path); got != want {
			t.Errorf("%q: got %v want %v", path, got, want)
		}
	}
}

// Every v0.1 fixture line with an exception must produce a stable, non-empty grouping.
func TestFromEventV01Fixture(t *testing.T) {
	f, err := os.Open("../model/testdata/sdk_v0.1.ndjson")
	if err != nil {
		t.Skip("fixture not found")
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	n := 0
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev model.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatal(err)
		}
		ev.Normalize()
		ev.ProjectID = "p"
		if !ev.IsError() {
			continue
		}
		occ := FromEvent(ev)
		again := FromEvent(ev)
		if occ.IssueID == "" || occ.IssueID != again.IssueID || occ.Title == "" {
			t.Fatalf("unstable grouping for %s", line)
		}
		n++
	}
	if n < 3 {
		t.Fatalf("expected error lines in fixture, got %d", n)
	}
}
