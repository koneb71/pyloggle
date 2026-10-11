package clickhouse

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ProjectUpsert is the user-supplied project configuration.
// Note: projects table is append-only; updates are modeled as inserts with a newer updated_at.
type ProjectUpsert struct {
	ID              string
	Name            string
	Description     string
	MaxEventsPerSec uint32
	MaxStorageGB    uint32
	RetentionDays   uint16 // 0 = the server retention; only shorter values take effect
}

type ProjectView struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	MaxEventsPerSec uint32    `json:"max_events_per_second"`
	MaxStorageGB    uint32    `json:"max_storage_gb"`
	RetentionDays   uint16    `json:"retention_days"`
	APIKeyHash      string    `json:"api_key_hash"`
	APIKeyCreatedAt time.Time `json:"api_key_created_at"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	IsDeleted       bool      `json:"is_deleted,omitempty"`
}

type projectViewRow struct {
	ID              string    `ch:"id"`
	Name            string    `ch:"name"`
	Description     string    `ch:"description"`
	MaxEventsPerSec uint32    `ch:"max_events_per_second"`
	MaxStorageGB    uint32    `ch:"max_storage_gb"`
	RetentionDays   uint16    `ch:"retention_days"`
	IsDeleted       uint8     `ch:"is_deleted"`
	APIKeyHash      string    `ch:"api_key_hash"`
	APIKeyCreatedAt time.Time `ch:"api_key_created_at"`
	CreatedAt       time.Time `ch:"created_at"`
	UpdatedAt       time.Time `ch:"updated_at"`
}

func (r projectViewRow) view() ProjectView {
	return ProjectView{ID: r.ID, Name: r.Name, Description: r.Description, MaxEventsPerSec: r.MaxEventsPerSec, MaxStorageGB: r.MaxStorageGB,
		RetentionDays: r.RetentionDays, APIKeyHash: r.APIKeyHash, APIKeyCreatedAt: r.APIKeyCreatedAt, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, IsDeleted: r.IsDeleted == 1}
}

// projects is append-only: every read takes the latest value per column (argMax by updated_at).
// Deleted projects keep a tombstone row and are filtered out of every normal read.
const projectLatest = `
	SELECT
		id,
		argMax(name, p.updated_at) AS name,
		argMax(description, p.updated_at) AS description,
		argMax(max_events_per_second, p.updated_at) AS max_events_per_second,
		argMax(max_storage_gb, p.updated_at) AS max_storage_gb,
		argMax(retention_days, p.updated_at) AS retention_days,
		argMax(is_deleted, p.updated_at) AS is_deleted,
		argMax(api_key_hash, p.updated_at) AS api_key_hash,
		argMax(api_key_created_at, p.updated_at) AS api_key_created_at,
		min(created_at) AS created_at,
		max(p.updated_at) AS updated_at
	FROM projects AS p`

const projectInsert = `
		INSERT INTO projects (
			id, name, description,
			max_events_per_second, max_storage_gb, retention_days, is_deleted,
			api_key_hash, api_key_created_at,
			created_at, updated_at
		)
	`

func (c *Client) ListProjects(ctx context.Context, limit int) ([]ProjectView, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	q := "SELECT * FROM (" + projectLatest + " GROUP BY id) WHERE is_deleted = 0 ORDER BY updated_at DESC LIMIT ?"
	rows := make([]projectViewRow, 0, limit)
	if err := c.conn.Select(ctx, &rows, q, limit); err != nil {
		return nil, err
	}
	out := make([]ProjectView, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.view())
	}
	return out, nil
}

// CreateProject stores the project and mints its "default" key (every scope). The key is
// returned once; only its hash is kept, in project_api_keys.
func (c *Client) CreateProject(ctx context.Context, p ProjectUpsert) (ProjectView, string, error) {
	if p.ID == "" || p.Name == "" {
		return ProjectView{}, "", errors.New("missing id/name")
	}
	now := time.Now().UTC()
	batch, err := c.conn.PrepareBatch(ctx, projectInsert)
	if err != nil {
		return ProjectView{}, "", err
	}
	if err := batch.Append(
		p.ID, p.Name, p.Description,
		p.MaxEventsPerSec, p.MaxStorageGB, p.RetentionDays, uint8(0),
		"", time.Time{},
		now, now,
	); err != nil {
		_ = batch.Abort()
		return ProjectView{}, "", err
	}
	if err := batch.Send(); err != nil {
		return ProjectView{}, "", err
	}
	_, apiKey, err := c.CreateAPIKey(ctx, p.ID, "default", []string{"ingest", "read", "write"}, "")
	if err != nil {
		return ProjectView{}, "", fmt.Errorf("default key: %w", err)
	}
	return ProjectView{
		ID:              p.ID,
		Name:            p.Name,
		Description:     p.Description,
		MaxEventsPerSec: p.MaxEventsPerSec,
		MaxStorageGB:    p.MaxStorageGB,
		RetentionDays:   p.RetentionDays,
		APIKeyCreatedAt: now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}, apiKey, nil
}

func (c *Client) UpsertProject(ctx context.Context, p ProjectUpsert) (ProjectView, error) {
	if p.ID == "" || p.Name == "" {
		return ProjectView{}, errors.New("missing id/name")
	}
	now := time.Now().UTC()
	// Preserve current API key (latest) while updating other fields.
	cur, ok, err := c.GetProjectByID(ctx, p.ID)
	if err != nil {
		return ProjectView{}, err
	}
	if !ok {
		return ProjectView{}, errors.New("project not found")
	}
	batch, err := c.conn.PrepareBatch(ctx, projectInsert)
	if err != nil {
		return ProjectView{}, err
	}
	if err := batch.Append(
		p.ID, p.Name, p.Description,
		p.MaxEventsPerSec, p.MaxStorageGB, p.RetentionDays, uint8(0),
		cur.APIKeyHash, cur.APIKeyCreatedAt,
		cur.CreatedAt, now,
	); err != nil {
		_ = batch.Abort()
		return ProjectView{}, err
	}
	if err := batch.Send(); err != nil {
		return ProjectView{}, err
	}
	cur.Name = p.Name
	cur.Description = p.Description
	cur.MaxEventsPerSec = p.MaxEventsPerSec
	cur.MaxStorageGB = p.MaxStorageGB
	cur.RetentionDays = p.RetentionDays
	cur.UpdatedAt = now
	return cur, nil
}

// MarkProjectDeleted writes the tombstone: the project vanishes from lists, lookups and key
// resolution at once. PurgeProjectData removes the rows afterwards.
func (c *Client) MarkProjectDeleted(ctx context.Context, projectID string) error {
	cur, ok, err := c.GetProjectByID(ctx, projectID)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("project not found")
	}
	now := time.Now().UTC()
	batch, err := c.conn.PrepareBatch(ctx, projectInsert)
	if err != nil {
		return err
	}
	if err := batch.Append(cur.ID, cur.Name, cur.Description, cur.MaxEventsPerSec, cur.MaxStorageGB, cur.RetentionDays, uint8(1),
		cur.APIKeyHash, cur.APIKeyCreatedAt, cur.CreatedAt, now); err != nil {
		_ = batch.Abort()
		return err
	}
	return batch.Send()
}

// RotateProjectAPIKey is the 0.2 rotation: a fresh "default" key with every scope replaces
// the active key(s) of that name; keys with other names are untouched. The revoked hashes
// are returned so the caller can drop them from the resolver.
func (c *Client) RotateProjectAPIKey(ctx context.Context, projectID string) (ProjectView, string, error) {
	if projectID == "" {
		return ProjectView{}, "", errors.New("missing project_id")
	}
	cur, ok, err := c.GetProjectByID(ctx, projectID)
	if err != nil {
		return ProjectView{}, "", err
	}
	if !ok {
		return ProjectView{}, "", errors.New("project not found")
	}
	old, err := c.ListAPIKeys(ctx, projectID)
	if err != nil {
		return ProjectView{}, "", err
	}
	_, apiKey, err := c.CreateAPIKey(ctx, projectID, "default", []string{"ingest", "read", "write"}, "rotate")
	if err != nil {
		return ProjectView{}, "", err
	}
	for _, k := range old {
		if k.RevokedAt == nil && k.Name == "default" {
			if _, err := c.RevokeAPIKey(ctx, projectID, k.ID, "rotate"); err != nil {
				return ProjectView{}, "", err
			}
		}
	}
	cur.APIKeyCreatedAt = time.Now().UTC()
	return cur, apiKey, nil
}

// GetProjectByID returns a live project (deleted ones read as absent).
func (c *Client) GetProjectByID(ctx context.Context, projectID string) (ProjectView, bool, error) {
	p, ok, err := c.GetProjectIncludingDeleted(ctx, projectID)
	if err != nil || !ok || p.IsDeleted {
		return ProjectView{}, false, err
	}
	return p, true, nil
}

// GetProjectIncludingDeleted also returns tombstoned projects (deletion status).
func (c *Client) GetProjectIncludingDeleted(ctx context.Context, projectID string) (ProjectView, bool, error) {
	if projectID == "" {
		return ProjectView{}, false, nil
	}
	var rows []projectViewRow
	if err := c.conn.Select(ctx, &rows, projectLatest+" WHERE id = ? GROUP BY id LIMIT 1", projectID); err != nil {
		return ProjectView{}, false, err
	}
	if len(rows) == 0 {
		return ProjectView{}, false, nil
	}
	return rows[0].view(), true, nil
}

// EnsureProject ensures that a project exists. If it does not, it is created.
//
// Note: project creation generates an API key, which is intentionally discarded here.
func (c *Client) EnsureProject(ctx context.Context, p ProjectUpsert) (bool, error) {
	p.ID = strings.TrimSpace(p.ID)
	p.Name = strings.TrimSpace(p.Name)
	if p.ID == "" {
		return false, errors.New("missing id")
	}
	if p.Name == "" {
		p.Name = p.ID
	}
	if _, ok, err := c.GetProjectByID(ctx, p.ID); err != nil {
		return false, fmt.Errorf("ensure project: get by id: %w", err)
	} else if ok {
		return false, nil
	}
	_, _, err := c.CreateProject(ctx, p)
	if err != nil {
		return false, fmt.Errorf("ensure project: create: %w", err)
	}
	return true, nil
}

func generateAPIKey() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	// URL-safe, no padding.
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func apiKeyHash(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:])
}
