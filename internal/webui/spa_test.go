package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pyloggle/internal/config"
)

func TestSPAHandlerRoutesAndAssets(t *testing.T) {
	u := NewForTest(config.Config{UIAuthMode: "disabled"}, nil, nil, nil)
	s := u.spa.(*spaHandler)
	if !s.built() {
		t.Skip("web/dist not built; run `make web`")
	}
	for _, p := range []string{"/ui/", "/ui/issues", "/ui/issues/abc", "/ui/logs?q=level:error", "/ui/login"} {
		rec := httptest.NewRecorder()
		u.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `<div id="root">`) {
			t.Fatalf("%s: code=%d body=%q", p, rec.Code, rec.Body.String()[:min(80, rec.Body.Len())])
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Fatalf("%s: index must not be cached, got %q", p, cc)
		}
	}
	// A hashed asset referenced by index.html is served with an immutable cache header.
	rec := httptest.NewRecorder()
	u.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/", nil))
	i := strings.Index(rec.Body.String(), "/ui/assets/")
	if i < 0 {
		t.Fatal("index.html references no /ui/assets/ file")
	}
	asset := rec.Body.String()[i:]
	asset = asset[:strings.IndexAny(asset, `"'`)]
	rec = httptest.NewRecorder()
	u.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, asset, nil))
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("asset %s: code=%d cache=%q", asset, rec.Code, rec.Header().Get("Cache-Control"))
	}
	// Unknown asset-looking paths fall back to the app (client router shows its own 404).
	rec = httptest.NewRecorder()
	u.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/assets/nope.js", nil))
	if rec.Code != 200 {
		t.Fatalf("fallback code=%d", rec.Code)
	}
}

func TestSPAUnbuiltExplains(t *testing.T) {
	s := &spaHandler{}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/issues", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "npm run build") {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAzureModeRedirectsAnonymousShell(t *testing.T) {
	u := NewForTest(config.Config{UIAuthMode: "azure", AzureTenantID: "t", AzureClientID: "c", AzureClientSecret: "s", UISessionSecret: "0123456789abcdef0123456789abcdef"}, nil, nil, nil)
	rec := httptest.NewRecorder()
	u.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/issues?status=open", nil))
	if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/ui/login?next=") {
		t.Fatalf("code=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
	// /ui/login then starts the OIDC flow (redirect to the identity provider).
	rec = httptest.NewRecorder()
	u.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui/login?next=%2Fui%2Fissues", nil))
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "oauth2") {
		t.Fatalf("login: code=%d loc=%q", rec.Code, rec.Header().Get("Location"))
	}
}
