package clickhouse

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"pyloggle/internal/api"
)

// API keys with scopes. project_api_keys is a ReplacingMergeTree keyed by (project_id, id):
// creation and revocation each insert a full row and reads take the latest per key, the same
// append-only pattern as projects. Last-use times live in their own table so the batched touch
// never races a revocation (a touch that rewrote the key row could resurrect a revoked key).
//
// Key format: "pl_" + 43 url-safe base64 characters; key_prefix keeps the first 11 so a key
// can be recognised in the UI. Only the SHA-256 of the full key is stored. Keys from 0.2 have
// no prefix and keep working: the migration copies each project's hash as its "default" key.

const (
	apiKeyPrefixLen  = 11
	apiKeyRandomSize = 32
)

// KeyRef identifies one key for the last-used batch.
type KeyRef struct {
	ProjectID string
	KeyID     string
}

// ResolvedKey is what an API key hash resolves to: the key and its live project.
type ResolvedKey struct {
	KeyID   string
	Name    string
	Scopes  []string
	Project ProjectRow
}

// createAPIKeys is the 015_api_keys migration step: the key table, the last-used table and a
// guarded backfill that turns each project's 0.2 key into a "default" key with every scope.
func createAPIKeys(ctx context.Context, c *Client, _ MigrateOptions) error {
	if err := c.Exec(ctx, `CREATE TABLE IF NOT EXISTS project_api_keys (
    project_id String,
    id String,
    name String,
    key_hash String,
    key_prefix String,
    scopes Array(String),
    created_at DateTime64(3, 'UTC'),
    created_by String,
    revoked_at DateTime64(3, 'UTC'),
    revoked_by String,
    updated_at DateTime64(3, 'UTC'),
    INDEX idx_hash key_hash TYPE bloom_filter(0.01) GRANULARITY 1
)
ENGINE = ReplacingMergeTree(updated_at)
ORDER BY (project_id, id)
SETTINGS index_granularity = 8192`); err != nil {
		return err
	}
	if err := c.Exec(ctx, `CREATE TABLE IF NOT EXISTS project_api_key_usage (
    project_id String,
    key_id String,
    last_used_at DateTime64(3, 'UTC')
)
ENGINE = ReplacingMergeTree(last_used_at)
ORDER BY (project_id, key_id)
SETTINGS index_granularity = 8192`); err != nil {
		return err
	}
	return c.Exec(ctx, `INSERT INTO project_api_keys (project_id, id, name, key_hash, key_prefix, scopes, created_at, created_by, revoked_at, revoked_by, updated_at)
SELECT id, 'default', 'default', api_key_hash, '', ['ingest', 'read', 'write'], api_key_created_at, 'migration', toDateTime64(0, 3, 'UTC'), '', now64(3)
FROM (
    SELECT id, argMax(api_key_hash, updated_at) AS api_key_hash, argMax(api_key_created_at, updated_at) AS api_key_created_at, argMax(is_deleted, updated_at) AS is_deleted
    FROM projects GROUP BY id
)
WHERE api_key_hash != '' AND is_deleted = 0 AND (SELECT count() FROM project_api_keys) = 0`)
}

// generateScopedAPIKey returns a fresh key, its hash and its display prefix.
func generateScopedAPIKey() (key, hash, prefix string, err error) {
	var b [apiKeyRandomSize]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", "", err
	}
	key = "pl_" + base64.RawURLEncoding.EncodeToString(b[:])
	return key, apiKeyHash(key), key[:apiKeyPrefixLen], nil
}

func newKeyID() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "k_" + hex.EncodeToString(b[:]), nil
}

// normalizeScopes validates and orders a scope list (ingest, read, write).
func normalizeScopes(scopes []string) ([]string, error) {
	order := map[string]int{"ingest": 0, "read": 1, "write": 2}
	seen := map[string]bool{}
	out := make([]string, 0, 3)
	for _, s := range scopes {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		if _, ok := order[s]; !ok {
			return nil, fmt.Errorf("unknown scope %q (ingest, read, write)", s)
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, errors.New("at least one scope is required")
	}
	sort.Slice(out, func(i, j int) bool { return order[out[i]] < order[out[j]] })
	return out, nil
}

const apiKeyInsert = `INSERT INTO project_api_keys (project_id, id, name, key_hash, key_prefix, scopes, created_at, created_by, revoked_at, revoked_by, updated_at)`

// CreateAPIKey mints a key for the project and returns it once, with the stored record.
func (c *Client) CreateAPIKey(ctx context.Context, projectID, name string, scopes []string, createdBy string) (api.APIKey, string, error) {
	projectID = strings.TrimSpace(projectID)
	name = strings.TrimSpace(name)
	if projectID == "" {
		return api.APIKey{}, "", errors.New("missing project_id")
	}
	if name == "" {
		name = "default"
	}
	if len(name) > 100 {
		return api.APIKey{}, "", errors.New("name is too long (100 characters)")
	}
	scopes, err := normalizeScopes(scopes)
	if err != nil {
		return api.APIKey{}, "", err
	}
	if _, ok, err := c.GetProjectByID(ctx, projectID); err != nil {
		return api.APIKey{}, "", err
	} else if !ok {
		return api.APIKey{}, "", errors.New("project not found")
	}
	key, hash, prefix, err := generateScopedAPIKey()
	if err != nil {
		return api.APIKey{}, "", err
	}
	id, err := newKeyID()
	if err != nil {
		return api.APIKey{}, "", err
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	batch, err := c.conn.PrepareBatch(ctx, apiKeyInsert)
	if err != nil {
		return api.APIKey{}, "", err
	}
	if err := batch.Append(projectID, id, name, hash, prefix, scopes, now, createdBy, time.Time{}, "", now); err != nil {
		_ = batch.Abort()
		return api.APIKey{}, "", err
	}
	if err := batch.Send(); err != nil {
		return api.APIKey{}, "", err
	}
	return api.APIKey{ID: id, ProjectID: projectID, Name: name, KeyPrefix: prefix, Scopes: scopes, CreatedAt: now, CreatedBy: createdBy}, key, nil
}

type apiKeyRow struct {
	api.APIKey
	hash      string
	revokedAt time.Time
}

// apiKeyLatest takes the latest value per key. The aliases differ from the column names on
// purpose: a SELECT alias shadows the column of the same name in WHERE, so "key_hash = ?"
// would otherwise filter on the aggregate.
const apiKeyLatest = `
	SELECT project_id, id,
		argMax(name, updated_at) AS latest_name, argMax(key_hash, updated_at) AS latest_hash, argMax(key_prefix, updated_at) AS latest_prefix,
		argMax(scopes, updated_at) AS latest_scopes, min(created_at) AS first_created_at, argMax(created_by, updated_at) AS latest_created_by,
		argMax(revoked_at, updated_at) AS latest_revoked_at, argMax(revoked_by, updated_at) AS latest_revoked_by
	FROM project_api_keys`

func (c *Client) apiKeyRows(ctx context.Context, where string, args ...any) ([]apiKeyRow, error) {
	rows, err := c.conn.Query(ctx, apiKeyLatest+" WHERE "+where+" GROUP BY project_id, id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []apiKeyRow
	for rows.Next() {
		var k apiKeyRow
		var createdAt, revokedAt time.Time
		if err := rows.Scan(&k.ProjectID, &k.ID, &k.Name, &k.hash, &k.KeyPrefix, &k.Scopes, &createdAt, &k.CreatedBy, &revokedAt, &k.RevokedBy); err != nil {
			return nil, err
		}
		k.CreatedAt = createdAt.UTC()
		k.revokedAt = revokedAt
		if revokedAt.Unix() > 0 {
			t := revokedAt.UTC()
			k.RevokedAt = &t
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// ListAPIKeys returns a project's keys, active ones first (newest first), then revoked ones.
func (c *Client) ListAPIKeys(ctx context.Context, projectID string) ([]api.APIKey, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("missing project_id")
	}
	rows, err := c.apiKeyRows(ctx, "project_id = ?", projectID)
	if err != nil {
		return nil, err
	}
	used := map[string]time.Time{}
	ur, err := c.conn.Query(ctx, "SELECT key_id, max(last_used_at) FROM project_api_key_usage WHERE project_id = ? GROUP BY key_id", projectID)
	if err != nil {
		return nil, err
	}
	for ur.Next() {
		var id string
		var at time.Time
		if err := ur.Scan(&id, &at); err != nil {
			ur.Close()
			return nil, err
		}
		if at.Unix() > 0 {
			used[id] = at.UTC()
		}
	}
	ur.Close()
	out := make([]api.APIKey, 0, len(rows))
	for _, r := range rows {
		k := r.APIKey
		if at, ok := used[k.ID]; ok {
			k.LastUsedAt = &at
		}
		if k.Scopes == nil {
			k.Scopes = []string{}
		}
		out = append(out, k)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := out[i].RevokedAt == nil, out[j].RevokedAt == nil
		if ai != aj {
			return ai
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// RevokeAPIKey marks a key revoked and returns its hash so the resolver can forget it at once.
// Revoking an already revoked key is a no-op.
func (c *Client) RevokeAPIKey(ctx context.Context, projectID, keyID, by string) (string, error) {
	rows, err := c.apiKeyRows(ctx, "project_id = ? AND id = ?", strings.TrimSpace(projectID), strings.TrimSpace(keyID))
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "", errors.New("key not found")
	}
	k := rows[0]
	if k.RevokedAt != nil {
		return k.hash, nil
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	batch, err := c.conn.PrepareBatch(ctx, apiKeyInsert)
	if err != nil {
		return "", err
	}
	if err := batch.Append(k.ProjectID, k.ID, k.Name, k.hash, k.KeyPrefix, k.Scopes, k.CreatedAt, k.CreatedBy, now, by, now); err != nil {
		_ = batch.Abort()
		return "", err
	}
	if err := batch.Send(); err != nil {
		return "", err
	}
	return k.hash, nil
}

// TouchAPIKeys records last-use times; the resolver batches these a few minutes at a time.
func (c *Client) TouchAPIKeys(ctx context.Context, lastUsed map[KeyRef]time.Time) error {
	if len(lastUsed) == 0 {
		return nil
	}
	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO project_api_key_usage (project_id, key_id, last_used_at)")
	if err != nil {
		return err
	}
	for ref, at := range lastUsed {
		if err := batch.Append(ref.ProjectID, ref.KeyID, at.UTC()); err != nil {
			_ = batch.Abort()
			return err
		}
	}
	return batch.Send()
}

// ResolveAPIKey finds the unrevoked key with this hash and its live project.
func (c *Client) ResolveAPIKey(ctx context.Context, keyHash string) (ResolvedKey, bool, error) {
	keyHash = strings.TrimSpace(keyHash)
	if keyHash == "" {
		return ResolvedKey{}, false, nil
	}
	rows, err := c.apiKeyRows(ctx, "key_hash = ?", keyHash)
	if err != nil {
		return ResolvedKey{}, false, err
	}
	for _, k := range rows {
		if k.RevokedAt != nil {
			continue
		}
		pv, ok, err := c.GetProjectByID(ctx, k.ProjectID)
		if err != nil {
			return ResolvedKey{}, false, err
		}
		if !ok {
			continue
		}
		return ResolvedKey{KeyID: k.ID, Name: k.Name, Scopes: k.Scopes,
			Project: ProjectRow{ID: pv.ID, Name: pv.Name, MaxEventsPerSec: pv.MaxEventsPerSec, MaxStorageGB: pv.MaxStorageGB, RetentionDays: pv.RetentionDays, APIKeyHash: keyHash}}, true, nil
	}
	return ResolvedKey{}, false, nil
}

// GetProjectByAPIKeyHash is the 0.2 lookup, now answered from project_api_keys.
func (c *Client) GetProjectByAPIKeyHash(ctx context.Context, apiKeyHash string) (ProjectRow, bool, error) {
	k, ok, err := c.ResolveAPIKey(ctx, apiKeyHash)
	if err != nil || !ok {
		return ProjectRow{}, false, err
	}
	return k.Project, true, nil
}
