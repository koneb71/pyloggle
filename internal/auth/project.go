package auth

import (
	"context"
	"net/http"

	"pyloggle/internal/config"
	"pyloggle/internal/tenant"
)

type ctxKey int

const (
	projectKey ctxKey = iota + 1
)

// WithProject attaches the resolved project to the request context.
func WithProject(ctx context.Context, p tenant.Project) context.Context {
	return context.WithValue(ctx, projectKey, p)
}

// ProjectFromContext returns the project if it was attached by RequireProject.
func ProjectFromContext(ctx context.Context) (tenant.Project, bool) {
	v := ctx.Value(projectKey)
	if v == nil {
		return tenant.Project{}, false
	}
	p, ok := v.(tenant.Project)
	return p, ok
}

func ProjectIDFromContext(ctx context.Context) string {
	if p, ok := ProjectFromContext(ctx); ok {
		return p.ID
	}
	return ""
}

// RequireProject enforces multi-tenant API key auth using X-API-Key.
//
// Behavior:
// - If X-API-Key is present: must map to a project (401 otherwise).
// - If X-API-Key is absent: allowed only when cfg.DefaultProjID is non-empty (single-tenant mode).
func RequireProject(cfg config.Config, res *tenant.Resolver, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			if cfg.DefaultProjID == "" {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := WithProject(r.Context(), tenant.Project{ID: cfg.DefaultProjID})
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		p, ok, err := res.ResolveByAPIKey(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "auth lookup failed", http.StatusInternalServerError)
			return
		}
		if !ok || p.ID == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := WithProject(r.Context(), p)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
