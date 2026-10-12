package config

import (
	"strings"
	"testing"
)

// clearSignIn unsets every variable that decides the sign-in mode.
func clearSignIn(t *testing.T) {
	for _, k := range []string{"PYLOGGLE_UI_AUTH", "PYLOGGLE_AZURE_TENANT_ID", "PYLOGGLE_AZURE_CLIENT_ID", "PYLOGGLE_AZURE_CLIENT_SECRET",
		"PYLOGGLE_OIDC_ISSUER", "PYLOGGLE_OIDC_CLIENT_ID", "PYLOGGLE_OIDC_CLIENT_SECRET", "PYLOGGLE_UI_SESSION_SECRET"} {
		t.Setenv(k, "")
	}
}

func TestSignInModeIsInferredOrRequired(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		mode string
		err  string // substring of Validate's error; "" means valid
	}{
		{"nothing set", nil, "", "sign-in is not configured"},
		{"tenant only", map[string]string{"PYLOGGLE_AZURE_TENANT_ID": "t", "PYLOGGLE_AZURE_CLIENT_ID": "c", "PYLOGGLE_AZURE_CLIENT_SECRET": "s",
			"PYLOGGLE_UI_SESSION_SECRET": strings.Repeat("x", 32)}, "azure", ""},
		{"issuer only", map[string]string{"PYLOGGLE_OIDC_ISSUER": "https://id.example", "PYLOGGLE_OIDC_CLIENT_ID": "c", "PYLOGGLE_OIDC_CLIENT_SECRET": "s",
			"PYLOGGLE_UI_SESSION_SECRET": "c2VjcmV0LXNlY3JldC1zZWNyZXQtMTI="}, "oidc", ""},
		{"both set", map[string]string{"PYLOGGLE_AZURE_TENANT_ID": "t", "PYLOGGLE_OIDC_ISSUER": "https://id.example"}, "", "choose one"},
		{"explicit, mixed case", map[string]string{"PYLOGGLE_UI_AUTH": " Disabled "}, "disabled", ""},
		{"typo", map[string]string{"PYLOGGLE_UI_AUTH": "disable"}, "disable", "is not a sign-in mode"},
		{"oidc missing settings", map[string]string{"PYLOGGLE_UI_AUTH": "oidc", "PYLOGGLE_OIDC_ISSUER": "https://id.example"}, "oidc",
			"set PYLOGGLE_OIDC_CLIENT_ID, PYLOGGLE_OIDC_CLIENT_SECRET, PYLOGGLE_UI_SESSION_SECRET"},
		{"short session secret", map[string]string{"PYLOGGLE_UI_AUTH": "azure", "PYLOGGLE_AZURE_TENANT_ID": "t", "PYLOGGLE_AZURE_CLIENT_ID": "c",
			"PYLOGGLE_AZURE_CLIENT_SECRET": "s", "PYLOGGLE_UI_SESSION_SECRET": "short"}, "azure", "at least 32 characters"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			clearSignIn(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			cfg := FromEnv()
			if cfg.UIAuthMode != c.mode {
				t.Fatalf("mode = %q, want %q", cfg.UIAuthMode, c.mode)
			}
			err := cfg.Validate()
			switch {
			case c.err == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
				t.Fatalf("error %v, want it to mention %q", err, c.err)
			}
		})
	}
}

func TestUnknownModeNeverMeansSignInOff(t *testing.T) {
	for _, mode := range []string{"", "disable", "none"} {
		if got := (Config{UIAuthMode: mode}).OIDC().Mode; got == "disabled" {
			t.Fatalf("mode %q resolved to disabled", mode)
		}
	}
	if got := (Config{UIAuthMode: "disabled"}).OIDC().Mode; got != "disabled" {
		t.Fatalf("explicit disabled: %q", got)
	}
}

func TestEmptyDefaultProjectRequiresKeys(t *testing.T) {
	t.Setenv("PYLOGGLE_DEFAULT_PROJECT_ID", "")
	if got := FromEnv().DefaultProjID; got != "" {
		t.Fatalf("an empty PYLOGGLE_DEFAULT_PROJECT_ID must stay empty, got %q", got)
	}
}
