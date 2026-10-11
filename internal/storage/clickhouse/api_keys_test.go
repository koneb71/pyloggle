package clickhouse

import (
	"strings"
	"testing"
)

func TestScopedAPIKeyFormat(t *testing.T) {
	key, hash, prefix, err := generateScopedAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "pl_") || len(key) != 3+43 || strings.ContainsAny(key, "+/=") {
		t.Fatalf("key format: %q", key)
	}
	if prefix != key[:11] || len(hash) != 64 || hash != apiKeyHash(key) {
		t.Fatalf("prefix/hash: %q %q", prefix, hash)
	}
	id, err := newKeyID()
	if err != nil || !strings.HasPrefix(id, "k_") || len(id) != 14 {
		t.Fatalf("key id: %q %v", id, err)
	}
}

func TestNormalizeScopes(t *testing.T) {
	got, err := normalizeScopes([]string{"write", " READ ", "write", ""})
	if err != nil || strings.Join(got, ",") != "read,write" {
		t.Fatalf("normalize: %v %v", got, err)
	}
	if _, err := normalizeScopes([]string{"admin"}); err == nil {
		t.Fatal("unknown scope accepted")
	}
	if _, err := normalizeScopes(nil); err == nil {
		t.Fatal("empty scopes accepted")
	}
	if got, _ := normalizeScopes([]string{"write", "ingest", "read"}); strings.Join(got, ",") != "ingest,read,write" {
		t.Fatalf("order: %v", got)
	}
}
