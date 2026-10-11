//go:build integration

package clickhouse

import (
	"context"
	"testing"
	"time"
)

func TestIntegrationAPIKeys(t *testing.T) {
	c, cleanup := integrationClient(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := c.Migrate(ctx, MigrateOptions{Rebuild: true, Logf: t.Logf}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Creating a project mints its default key with every scope.
	pv, secret, err := c.CreateProject(ctx, ProjectUpsert{ID: "p1", Name: "One", MaxStorageGB: 3})
	if err != nil || secret == "" {
		t.Fatalf("create project: %v", err)
	}
	if pv.APIKeyHash != "" {
		t.Fatalf("projects.api_key_hash must no longer be written: %q", pv.APIKeyHash)
	}
	k, ok, err := c.ResolveAPIKey(ctx, apiKeyHash(secret))
	if err != nil || !ok || k.Name != "default" || len(k.Scopes) != 3 || k.Project.ID != "p1" || k.Project.MaxStorageGB != 3 {
		t.Fatalf("default key: %+v %v %v", k, ok, err)
	}
	if _, ok, _ := c.GetProjectByAPIKeyHash(ctx, apiKeyHash(secret)); !ok {
		t.Fatal("0.2 lookup must still resolve")
	}

	// A read-only key resolves with its scopes; an unknown hash does not.
	ro, roSecret, err := c.CreateAPIKey(ctx, "p1", "dashboards", []string{"read"}, "ada@example.com")
	if err != nil || len(roSecret) != 46 || ro.KeyPrefix != roSecret[:11] {
		t.Fatalf("create read key: %+v %q %v", ro, roSecret, err)
	}
	if k, ok, _ := c.ResolveAPIKey(ctx, apiKeyHash(roSecret)); !ok || k.KeyID != ro.ID || len(k.Scopes) != 1 || k.Scopes[0] != "read" {
		t.Fatalf("read key: %+v %v", k, ok)
	}
	if _, ok, _ := c.ResolveAPIKey(ctx, apiKeyHash("pl_nope")); ok {
		t.Fatal("unknown key resolved")
	}
	if _, _, err := c.CreateAPIKey(ctx, "missing", "x", []string{"read"}, ""); err == nil {
		t.Fatal("key for an unknown project")
	}
	if _, _, err := c.CreateAPIKey(ctx, "p1", "x", []string{"root"}, ""); err == nil {
		t.Fatal("unknown scope accepted")
	}

	// Last-used times land through the batched touch and show in the list.
	used := time.Now().UTC().Truncate(time.Millisecond)
	if err := c.TouchAPIKeys(ctx, map[KeyRef]time.Time{{ProjectID: "p1", KeyID: ro.ID}: used}); err != nil {
		t.Fatalf("touch: %v", err)
	}
	list, err := c.ListAPIKeys(ctx, "p1")
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %v %v", list, err)
	}
	if list[0].ID != ro.ID || list[0].LastUsedAt == nil || !list[0].LastUsedAt.Equal(used) || list[1].Name != "default" || list[1].LastUsedAt != nil {
		t.Fatalf("list order/last used: %+v", list)
	}

	// Revocation: the key stops resolving, stays listed (after the active ones), is idempotent.
	hash, err := c.RevokeAPIKey(ctx, "p1", ro.ID, "ada@example.com")
	if err != nil || hash != apiKeyHash(roSecret) {
		t.Fatalf("revoke: %q %v", hash, err)
	}
	if _, ok, _ := c.ResolveAPIKey(ctx, apiKeyHash(roSecret)); ok {
		t.Fatal("revoked key still resolves")
	}
	if _, err := c.RevokeAPIKey(ctx, "p1", ro.ID, "again"); err != nil {
		t.Fatalf("second revoke: %v", err)
	}
	if _, err := c.RevokeAPIKey(ctx, "p1", "k_missing", ""); err == nil {
		t.Fatal("revoking an unknown key succeeded")
	}
	list, _ = c.ListAPIKeys(ctx, "p1")
	if len(list) != 2 || list[0].Name != "default" || list[1].RevokedAt == nil || list[1].RevokedBy != "ada@example.com" {
		t.Fatalf("list after revoke: %+v", list)
	}

	// Rotation replaces the default key and leaves other named keys alone.
	ci, ciSecret, _ := c.CreateAPIKey(ctx, "p1", "ci", []string{"ingest"}, "")
	_, rotated, err := c.RotateProjectAPIKey(ctx, "p1")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, ok, _ := c.ResolveAPIKey(ctx, apiKeyHash(secret)); ok {
		t.Fatal("old default key still resolves after rotation")
	}
	if k, ok, _ := c.ResolveAPIKey(ctx, apiKeyHash(rotated)); !ok || k.Name != "default" || len(k.Scopes) != 3 {
		t.Fatalf("new default: %+v %v", k, ok)
	}
	if k, ok, _ := c.ResolveAPIKey(ctx, apiKeyHash(ciSecret)); !ok || k.KeyID != ci.ID {
		t.Fatal("rotation must not touch other keys")
	}

	// A deleted project's keys stop resolving at once and are purged with the project.
	if err := c.MarkProjectDeleted(ctx, "p1"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.ResolveAPIKey(ctx, apiKeyHash(rotated)); ok {
		t.Fatal("key of a deleted project resolves")
	}
	if err := c.PurgeProjectData(ctx, "p1"); err != nil {
		t.Fatalf("purge: %v", err)
	}
	var n uint64
	_ = c.QueryRow(ctx, "SELECT count() FROM project_api_keys WHERE project_id = 'p1'").Scan(&n)
	if n != 0 {
		t.Fatalf("key rows left after purge: %d", n)
	}
}
