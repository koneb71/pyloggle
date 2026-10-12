package clickhouse

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/koneb71/pyloggle/internal/api"
)

type savedSearchRow struct {
	ID          string    `ch:"id"`
	ProjectID   string    `ch:"project_id"`
	UserID      string    `ch:"user_id"`
	Name        string    `ch:"name"`
	Description string    `ch:"description"`
	Query       string    `ch:"query"`
	IsPublic    uint8     `ch:"is_public"`
	IsPinned    uint8     `ch:"is_pinned"`
	IsDeleted   uint8     `ch:"is_deleted"`
	CreatedAt   time.Time `ch:"created_at"`
	UpdatedAt   time.Time `ch:"updated_at"`
	LastUsedAt  time.Time `ch:"last_used_at"`
}

func (r savedSearchRow) toAPI() api.SavedSearch {
	return api.SavedSearch{
		ID:          r.ID,
		ProjectID:   r.ProjectID,
		UserID:      r.UserID,
		Name:        r.Name,
		Description: r.Description,
		Query:       r.Query,
		IsPublic:    r.IsPublic == 1,
		IsPinned:    r.IsPinned == 1,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
		LastUsedAt:  r.LastUsedAt,
	}
}

func (c *Client) GetSavedSearch(ctx context.Context, projectID, id string) (api.SavedSearch, bool, error) {
	if projectID == "" || id == "" {
		return api.SavedSearch{}, false, nil
	}
	q := `
		SELECT
			id,
			project_id,
			argMax(user_id, s.updated_at) as user_id,
			argMax(name, s.updated_at) as name,
			argMax(description, s.updated_at) as description,
			argMax(query, s.updated_at) as query,
			argMax(is_public, s.updated_at) as is_public,
			argMax(is_pinned, s.updated_at) as is_pinned,
			argMax(is_deleted, s.updated_at) as is_deleted,
			min(created_at) as created_at,
			max(s.updated_at) as updated_at,
			max(last_used_at) as last_used_at
		FROM saved_searches AS s
		WHERE project_id = ?
		  AND id = ?
		GROUP BY project_id, id
		HAVING is_deleted = 0
		LIMIT 1
	`
	var rows []savedSearchRow
	if err := c.conn.Select(ctx, &rows, q, projectID, id); err != nil {
		return api.SavedSearch{}, false, err
	}
	if len(rows) == 0 {
		return api.SavedSearch{}, false, nil
	}
	return rows[0].toAPI(), true, nil
}

func (c *Client) ListSavedSearches(ctx context.Context, projectID, userID string, includePublic bool, onlyPinned bool, limit int) ([]api.SavedSearch, error) {
	if projectID == "" {
		return nil, errors.New("missing project_id")
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	// Visibility is decided on the latest version of each row (the argMax aliases), so the
	// filter belongs in HAVING, not WHERE: ClickHouse resolves "user_id" there to the aggregate.
	havingUser := ""
	args := make([]any, 0, 4)
	args = append(args, projectID)
	if userID != "" && includePublic {
		havingUser = " AND (user_id = ? OR is_public = 1)\n"
		args = append(args, userID)
	} else if userID != "" {
		havingUser = " AND user_id = ?\n"
		args = append(args, userID)
	} else if includePublic {
		havingUser = " AND is_public = 1\n"
	}

	q := `
		SELECT
			id,
			project_id,
			argMax(user_id, s.updated_at) as user_id,
			argMax(name, s.updated_at) as name,
			argMax(description, s.updated_at) as description,
			argMax(query, s.updated_at) as query,
			argMax(is_public, s.updated_at) as is_public,
			argMax(is_pinned, s.updated_at) as is_pinned,
			argMax(is_deleted, s.updated_at) as is_deleted,
			min(created_at) as created_at,
			max(s.updated_at) as updated_at,
			max(last_used_at) as last_used_at
		FROM saved_searches AS s
		WHERE project_id = ?
		GROUP BY project_id, id
		HAVING is_deleted = 0
		` + havingUser + optionalPinnedHaving(onlyPinned) + `
		ORDER BY is_pinned DESC, updated_at DESC
		LIMIT ?
	`

	rows := make([]savedSearchRow, 0, limit)
	args = append(args, limit)
	if err := c.conn.Select(ctx, &rows, q, args...); err != nil {
		return nil, err
	}
	out := make([]api.SavedSearch, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toAPI())
	}
	return out, nil
}

func (c *Client) UpsertSavedSearch(ctx context.Context, s api.SavedSearch) (api.SavedSearch, error) {
	now := time.Now().UTC()
	if s.ID == "" {
		s.ID = newID()
	}
	if s.ProjectID == "" {
		return api.SavedSearch{}, errors.New("missing project_id")
	}
	if s.UserID == "" {
		return api.SavedSearch{}, errors.New("missing user_id")
	}
	if strings.TrimSpace(s.Name) == "" {
		return api.SavedSearch{}, errors.New("missing name")
	}
	if strings.TrimSpace(s.Query) == "" {
		return api.SavedSearch{}, errors.New("missing query")
	}

	createdAt := s.CreatedAt
	lastUsedAt := s.LastUsedAt

	// Preserve created_at/last_used_at if the search already exists.
	if existing, ok, err := c.GetSavedSearch(ctx, s.ProjectID, s.ID); err == nil && ok {
		if createdAt.IsZero() {
			createdAt = existing.CreatedAt
		}
		if lastUsedAt.IsZero() && !existing.LastUsedAt.IsZero() {
			lastUsedAt = existing.LastUsedAt
		}
	}
	if createdAt.IsZero() {
		createdAt = now
	}
	if lastUsedAt.IsZero() {
		lastUsedAt = time.Time{} // keep zero unless explicitly set/touched
	}
	updatedAt := now

	batch, err := c.conn.PrepareBatch(ctx, `
		INSERT INTO saved_searches (
			id, project_id, user_id,
			name, description, query,
			is_public, is_pinned, is_deleted,
			created_at, updated_at, last_used_at
		)
	`)
	if err != nil {
		return api.SavedSearch{}, err
	}
	if err := batch.Append(
		s.ID,
		s.ProjectID,
		s.UserID,
		s.Name,
		s.Description,
		s.Query,
		boolToU8(s.IsPublic),
		boolToU8(s.IsPinned),
		uint8(0),
		createdAt,
		updatedAt,
		lastUsedAt,
	); err != nil {
		_ = batch.Abort()
		return api.SavedSearch{}, err
	}
	if err := batch.Send(); err != nil {
		return api.SavedSearch{}, err
	}

	s.CreatedAt = createdAt
	s.UpdatedAt = updatedAt
	s.LastUsedAt = lastUsedAt
	return s, nil
}

func (c *Client) SoftDeleteSavedSearch(ctx context.Context, projectID, userID, id string) error {
	now := time.Now().UTC()
	if projectID == "" || userID == "" || id == "" {
		return errors.New("missing project_id/user_id/id")
	}

	createdAt := now
	lastUsedAt := time.Time{}
	if existing, ok, err := c.GetSavedSearch(ctx, projectID, id); err == nil && ok {
		createdAt = existing.CreatedAt
		lastUsedAt = existing.LastUsedAt
	}

	batch, err := c.conn.PrepareBatch(ctx, `
		INSERT INTO saved_searches (
			id, project_id, user_id,
			name, description, query,
			is_public, is_pinned, is_deleted,
			created_at, updated_at, last_used_at
		)
	`)
	if err != nil {
		return err
	}
	if err := batch.Append(
		id,
		projectID,
		userID,
		"", // tombstone
		"",
		"",
		uint8(0),
		uint8(0),
		uint8(1),
		createdAt,
		now,
		lastUsedAt,
	); err != nil {
		_ = batch.Abort()
		return err
	}
	return batch.Send()
}

func (c *Client) TouchSavedSearchLastUsed(ctx context.Context, projectID, id string) error {
	now := time.Now().UTC()
	if projectID == "" || id == "" {
		return errors.New("missing project_id/id")
	}
	existing, ok, err := c.GetSavedSearch(ctx, projectID, id)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	batch, err := c.conn.PrepareBatch(ctx, `
		INSERT INTO saved_searches (
			id, project_id, user_id,
			name, description, query,
			is_public, is_pinned, is_deleted,
			created_at, updated_at, last_used_at
		)
	`)
	if err != nil {
		return err
	}
	if err := batch.Append(
		existing.ID,
		existing.ProjectID,
		existing.UserID,
		existing.Name,
		existing.Description,
		existing.Query,
		boolToU8(existing.IsPublic),
		boolToU8(existing.IsPinned),
		uint8(0),
		existing.CreatedAt,
		now,
		now,
	); err != nil {
		_ = batch.Abort()
		return err
	}
	return batch.Send()
}

func optionalPinnedHaving(onlyPinned bool) string {
	if !onlyPinned {
		return ""
	}
	return " AND is_pinned = 1\n"
}
