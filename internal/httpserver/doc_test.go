package httpserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestHTTPAPIDocListsEveryRoute keeps docs/reference/http-api.md complete: every route the
// server registers appears there as `METHOD /path`.
func TestHTTPAPIDocListsEveryRoute(t *testing.T) {
	b, err := os.ReadFile("../../docs/reference/http-api.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	// Routers and redirects, not endpoints anyone calls.
	skip := map[string]bool{"/": true, "/ui": true, "/ui/": true, "/v1/q/": true}

	var routes []string
	for _, args := range stringCalls(t, "../webui/api.go", "reg") {
		if len(args) >= 2 {
			routes = append(routes, args[0]+" "+args[1])
		}
	}
	if len(routes) < 50 {
		t.Fatalf("found only %d reg(...) routes in webui/api.go; the parser is missing calls", len(routes))
	}
	for _, fn := range []string{"Handle", "HandleFunc"} {
		for _, args := range stringCalls(t, "server.go", fn) {
			routes = append(routes, args[0])
		}
	}
	for _, args := range stringCalls(t, "../webui/webui.go", "") {
		if strings.HasPrefix(args[0], "/ui/") {
			routes = append(routes, args[0])
		}
	}

	for _, route := range routes {
		method, path, ok := strings.Cut(route, " ")
		if !ok {
			method, path = "[A-Z]+", route
		}
		if skip[path] {
			continue
		}
		re := regexp.MustCompile("`" + method + " " + regexp.QuoteMeta(path) + "`")
		if !re.MatchString(doc) {
			t.Errorf("docs/reference/http-api.md does not list %s", route)
		}
	}
}

// stringCalls returns the string-literal arguments of every call to fn in the file (fn ""
// collects the string literals of switch cases instead).
func stringCalls(t *testing.T, path, fn string) [][]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out [][]string
	ast.Inspect(f, func(n ast.Node) bool {
		var exprs []ast.Expr
		switch n := n.(type) {
		case *ast.CallExpr:
			name := ""
			switch f := n.Fun.(type) {
			case *ast.Ident:
				name = f.Name
			case *ast.SelectorExpr:
				name = f.Sel.Name
			}
			if fn == "" || name != fn {
				return true
			}
			exprs = n.Args
		case *ast.CaseClause:
			if fn != "" {
				return true
			}
			exprs = n.List
		default:
			return true
		}
		var args []string
		for _, e := range exprs {
			lit, ok := e.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				break
			}
			v, _ := strconv.Unquote(lit.Value)
			args = append(args, v)
		}
		if len(args) > 0 {
			out = append(out, args)
		}
		return true
	})
	return out
}
