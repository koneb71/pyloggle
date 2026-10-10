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
}

type ProjectView struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	MaxEventsPerSec uint32    `json:"max_events_per_second"`
	MaxStorageGB    uint32    `json:"max_storage_gb"`
	APIKeyHash      string    `json:"api_key_hash"`
	APIKeyCreatedAt time.Time `json:"api_key_created_at"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type projectViewRow struct {
	ID              string    `ch:"id"`
	Name            string    `ch:"name"`
	Description     string    `ch:"description"`
	MaxEventsPerSec uint32    `ch:"max_events_per_second"`
	MaxStorageGB    uint32    `ch:"max_storage_gb"`
	APIKeyHash      string    `ch:"api_key_hash"`
	APIKeyCreatedAt time.Time `ch:"api_key_created_at"`
	CreatedAt       time.Time `ch:"created_at"`
	UpdatedAt       time.Time `ch:"updated_at"`
}

func (c *Client) ListProjects(ctx context.Context, limit int) ([]ProjectView, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	// Latest per id.
	q := `
		SELECT
			id,
			argMax(name, p.updated_at) as name,
			argMax(description, p.updated_at) as description,
			argMax(max_events_per_second, p.updated_at) as max_events_per_second,
			argMax(max_storage_gb, p.updated_at) as max_storage_gb,
			argMax(api_key_hash, p.updated_at) as api_key_hash,
			argMax(api_key_created_at, p.updated_at) as api_key_created_at,
			min(created_at) as created_at,
			max(p.updated_at) as updated_at
		FROM projects AS p
		GROUP BY id
		ORDER BY updated_at DESC
		LIMIT ?
	`
	rows := make([]projectViewRow, 0, limit)
	if err := c.conn.Select(ctx, &rows, q, limit); err != nil {
		return nil, err
	}
	out := make([]ProjectView, 0, len(rows))
	for _, r := range rows {
		out = append(out, ProjectView{
			ID:              r.ID,
			Name:            r.Name,
			Description:     r.Description,
			MaxEventsPerSec: r.MaxEventsPerSec,
			MaxStorageGB:    r.MaxStorageGB,
			APIKeyHash:      r.APIKeyHash,
			APIKeyCreatedAt: r.APIKeyCreatedAt,
			CreatedAt:       r.CreatedAt,
			UpdatedAt:       r.UpdatedAt,
		})
	}
	return out, nil
}

func (c *Client) CreateProject(ctx context.Context, p ProjectUpsert) (ProjectView, string, error) {
	if p.ID == "" || p.Name == "" {
		return ProjectView{}, "", errors.New("missing id/name")
	}
	apiKey, err := generateAPIKey()
	if err != nil {
		return ProjectView{}, "", err
	}
	now := time.Now().UTC()
	apiKeyHash := apiKeyHash(apiKey)
	batch, err := c.conn.PrepareBatch(ctx, `
		INSERT INTO projects (
			id, name, description,
			max_events_per_second, max_storage_gb,
			api_key_hash, api_key_created_at,
			created_at, updated_at
		)
	`)
	if err != nil {
		return ProjectView{}, "", err
	}
	if err := batch.Append(
		p.ID, p.Name, p.Description,
		p.MaxEventsPerSec, p.MaxStorageGB,
		apiKeyHash, now,
		now, now,
	); err != nil {
		_ = batch.Abort()
		return ProjectView{}, "", err
	}
	if err := batch.Send(); err != nil {
		return ProjectView{}, "", err
	}
	return ProjectView{
		ID:              p.ID,
		Name:            p.Name,
		Description:     p.Description,
		MaxEventsPerSec: p.MaxEventsPerSec,
		MaxStorageGB:    p.MaxStorageGB,
		APIKeyHash:      apiKeyHash,
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
	batch, err := c.conn.PrepareBatch(ctx, `
		INSERT INTO projects (
			id, name, description,
			max_events_per_second, max_storage_gb,
			api_key_hash, api_key_created_at,
			created_at, updated_at
		)
	`)
	if err != nil {
		return ProjectView{}, err
	}
	if err := batch.Append(
		p.ID, p.Name, p.Description,
		p.MaxEventsPerSec, p.MaxStorageGB,
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
	cur.UpdatedAt = now
	return cur, nil
}

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
	apiKey, err := generateAPIKey()
	if err != nil {
		return ProjectView{}, "", err
	}
	now := time.Now().UTC()
	apiKeyHash := apiKeyHash(apiKey)

	batch, err := c.conn.PrepareBatch(ctx, `
		INSERT INTO projects (
			id, name, description,
			max_events_per_second, max_storage_gb,
			api_key_hash, api_key_created_at,
			created_at, updated_at
		)
	`)
	if err != nil {
		return ProjectView{}, "", err
	}
	if err := batch.Append(
		cur.ID, cur.Name, cur.Description,
		cur.MaxEventsPerSec, cur.MaxStorageGB,
		apiKeyHash, now,
		cur.CreatedAt, now,
	); err != nil {
		_ = batch.Abort()
		return ProjectView{}, "", err
	}
	if err := batch.Send(); err != nil {
		return ProjectView{}, "", err
	}
	cur.APIKeyHash = apiKeyHash
	cur.APIKeyCreatedAt = now
	cur.UpdatedAt = now
	return cur, apiKey, nil
}

func (c *Client) GetProjectByID(ctx context.Context, projectID string) (ProjectView, bool, error) {
	if projectID == "" {
		return ProjectView{}, false, nil
	}
	q := `
		SELECT
			id,
			argMax(name, p.updated_at) as name,
			argMax(description, p.updated_at) as description,
			argMax(max_events_per_second, p.updated_at) as max_events_per_second,
			argMax(max_storage_gb, p.updated_at) as max_storage_gb,
			argMax(api_key_hash, p.updated_at) as api_key_hash,
			argMax(api_key_created_at, p.updated_at) as api_key_created_at,
			min(created_at) as created_at,
			max(p.updated_at) as updated_at
		FROM projects AS p
		WHERE id = ?
		GROUP BY id
		LIMIT 1
	`
	var rows []projectViewRow
	if err := c.conn.Select(ctx, &rows, q, projectID); err != nil {
		return ProjectView{}, false, err
	}
	if len(rows) == 0 {
		return ProjectView{}, false, nil
	}
	r := rows[0]
	return ProjectView{
		ID:              r.ID,
		Name:            r.Name,
		Description:     r.Description,
		MaxEventsPerSec: r.MaxEventsPerSec,
		MaxStorageGB:    r.MaxStorageGB,
		APIKeyHash:      r.APIKeyHash,
		APIKeyCreatedAt: r.APIKeyCreatedAt,
		CreatedAt:       r.CreatedAt,
		UpdatedAt:       r.UpdatedAt,
	}, true, nil
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
