package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/koneb71/pyloggle/internal/config"
)

func TestKeylessReadsOnlyWithoutSignIn(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	for _, c := range []struct {
		mode, project string
		want          int
	}{
		{"disabled", "default", http.StatusOK},
		{"disabled", "", http.StatusUnauthorized},
		{"oidc", "default", http.StatusUnauthorized},
		{"azure", "default", http.StatusUnauthorized},
		{"", "default", http.StatusUnauthorized}, // not configured: fail closed
	} {
		h := RequireProject(config.Config{UIAuthMode: c.mode, DefaultProjID: c.project}, nil, ok)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/issues", nil))
		if rec.Code != c.want {
			t.Fatalf("mode=%q project=%q: %d, want %d", c.mode, c.project, rec.Code, c.want)
		}
	}
}
