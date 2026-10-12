package auth

import (
	"crypto/subtle"
	"net/http"
)

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
