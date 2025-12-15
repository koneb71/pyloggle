package auth

import (
	"crypto/subtle"
	"net/http"
)

// MaybeSharedSecret enforces a simple shared secret (X-API-Key) if configured.
// This is a lightweight v1 option; multi-tenant API keys will be added on top.
func MaybeSharedSecret(sharedSecret string, next http.Handler) http.Handler {
	if sharedSecret == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		if subtle.ConstantTimeCompare([]byte(key), []byte(sharedSecret)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdminToken enforces an admin token for sensitive endpoints.
// Use header: X-Admin-Token
func RequireAdminToken(adminToken string, next http.Handler) http.Handler {
	if adminToken == "" {
		// If not configured, deny by default (safer).
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "admin token not configured", http.StatusForbidden)
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-Admin-Token")
		if subtle.ConstantTimeCompare([]byte(key), []byte(adminToken)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}


