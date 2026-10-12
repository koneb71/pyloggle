package config

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"testing"
)

// docOnly are variables the configuration page documents that the Go runtime, not this
// package, reads.
var docOnly = map[string]bool{"GOMEMLIMIT": true}

// TestConfigurationDocMatchesCode keeps docs/reference/configuration.md complete: every
// environment variable config.go reads has a row, and every row is a variable that is read.
func TestConfigurationDocMatchesCode(t *testing.T) {
	read := envReadsIn(t, "config.go")
	documented := documentedVars(t, "../../docs/reference/configuration.md")

	for _, name := range sortedKeys(read) {
		if !documented[name] {
			t.Errorf("%s is read in config.go but has no row in docs/reference/configuration.md", name)
		}
	}
	for _, name := range sortedKeys(documented) {
		if !read[name] && !docOnly[name] {
			t.Errorf("docs/reference/configuration.md documents %s, which config.go does not read", name)
		}
	}
	if len(read) < 50 {
		t.Fatalf("found only %d variables in config.go; the parser is missing calls", len(read))
	}
}

// envReadsIn returns the string-literal first arguments of every os.Getenv, os.LookupEnv and
// getenv* call in the file.
func envReadsIn(t *testing.T, path string) map[string]bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	readers := map[string]bool{"Getenv": true, "LookupEnv": true, "getenv": true, "getenvInt": true, "getenvBool": true, "getenvFloat": true, "getenvDuration": true}
	out := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		var name string
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			name = fn.Name
		case *ast.SelectorExpr:
			name = fn.Sel.Name
		}
		lit, ok := call.Args[0].(*ast.BasicLit)
		if !readers[name] || !ok || lit.Kind != token.STRING {
			return true
		}
		if v, err := strconv.Unquote(lit.Value); err == nil {
			out[v] = true
		}
		return true
	})
	return out
}

// documentedVars returns the variable named in the first column of every table row.
func documentedVars(t *testing.T, path string) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, m := range regexp.MustCompile("(?m)^\\| `([A-Z][A-Z0-9_]+)` \\|").FindAllStringSubmatch(string(b), -1) {
		out[m[1]] = true
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
