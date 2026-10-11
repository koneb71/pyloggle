package tenant

import (
	"crypto/sha256"
	"encoding/hex"
)

// Scopes an API key can carry. A key resolves to a Project whose Scopes decide what the
// request may do; trusted paths (the UI session, the keyless default project, the shared
// secret) carry AllScopes explicitly.
const (
	ScopeIngest = "ingest" // POST /v1/ingest and the OTLP doors
	ScopeRead   = "read"   // every GET on /v1/*, /v1/q/* and /ui/api/*
	ScopeWrite  = "write"  // issue state, comments, saved views, alert rules
)

// AllScopes is the full set, in display order.
var AllScopes = []string{ScopeIngest, ScopeRead, ScopeWrite}

// ValidScope reports whether s names a scope.
func ValidScope(s string) bool {
	for _, k := range AllScopes {
		if k == s {
			return true
		}
	}
	return false
}

// Project is the resolved tenant of a request.
type Project struct {
	ID              string
	Name            string
	MaxEventsPerSec uint32
	MaxStorageGB    uint32
	APIKeyHash      string
	// KeyID names the API key that resolved the project ("" for keyless access).
	KeyID string
	// Scopes is what the request may do. It is never implied: an empty list allows nothing.
	Scopes []string
}

// Has reports whether the project's credentials carry the scope.
func (p Project) Has(scope string) bool {
	for _, s := range p.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Trusted returns the project with every scope (session users, the default project).
func Trusted(p Project) Project {
	p.Scopes = AllScopes
	return p
}

func APIKeyHash(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:])
}
