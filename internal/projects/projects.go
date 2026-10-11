package projects

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/projecthub/projecthub/internal/ids"
)

var (
	ErrInvalidID        = errors.New("invalid project or user ID")
	ErrInvalidInput     = errors.New("invalid project input")
	ErrNotFound         = errors.New("project not found")
	ErrConflict         = errors.New("project already exists")
	ErrCategoryNotFound = errors.New("category not found")
	ErrUserNotFound     = errors.New("user not found")
)

const (
	defaultPageSize = 50
	maxPageSize     = 100
	maxProjectLinks = 20
)

type Link struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	URL       string `json:"url"`
	Position  int    `json:"position"`
	CreatedAt int64  `json:"created_at"`
}

type LinkInput struct {
	Label string
	URL   string
}

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	CategoryID  string `json:"category_id"`
	SortOrder   int    `json:"sort_order"`
	Favorite    bool   `json:"favorite"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	Links       []Link `json:"links"`
}

type CreateInput struct {
	Name        string
	Description string
	Icon        string
	CategoryID  string
	SortOrder   int
	Links       []LinkInput
}

type UpdateInput = CreateInput

type ListFilter struct {
	Query         string
	CategoryID    string
	FavoritesOnly bool
	SortBy        string
	SortDirection string
	Limit         int
	Offset        int
}

type Service struct{ db *sql.DB }

func NewService(db *sql.DB) *Service { return &Service{db: db} }

func (s *Service) Create(ctx context.Context, input CreateInput) (Project, error) {
	input, err := normalize(input)
	if err != nil {
		return Project{}, err
	}
	if err := s.ensureNameAvailable(ctx, input.Name, ""); err != nil {
		return Project{}, err
	}
	if err := s.ensureCategory(ctx, input.CategoryID); err != nil {
		return Project{}, err
	}
	id, err := ids.NewUUID()
	if err != nil {
		return Project{}, fmt.Errorf("generate project ID: %w", err)
	}
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin project creation: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects (id, name, description, icon, category_id, sort_order, created_at, updated_at) VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?)`, id, input.Name, input.Description, input.Icon, input.CategoryID, input.SortOrder, now, now); err != nil {
		return Project{}, fmt.Errorf("insert project: %w", err)
	}
	if err := insertLinks(ctx, tx, id, input.Links, now); err != nil {
		return Project{}, err
	}
	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit project creation: %w", err)
	}
	return s.Get(ctx, id, "")
}

func (s *Service) Get(ctx context.Context, projectID, userID string) (Project, error) {
	if !ids.IsUUID(projectID) || (userID != "" && !ids.IsUUID(userID)) {
		return Project{}, ErrInvalidID
	}
	if userID != "" {
		if err := s.ensureUser(ctx, userID); err != nil {
			return Project{}, err
		}
	}
	item, err := s.get(ctx, projectID, userID)
	if err != nil {
		return Project{}, err
	}
	item.Links, err = s.links(ctx, projectID)
	if err != nil {
		return Project{}, err
	}
	return item, nil
}

func (s *Service) List(ctx context.Context, userID string, filter ListFilter) ([]Project, error) {
	if userID != "" {
		if !ids.IsUUID(userID) {
			return nil, ErrInvalidID
		}
		if err := s.ensureUser(ctx, userID); err != nil {
			return nil, err
		}
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if utf8.RuneCountInString(filter.Query) > 200 || strings.ContainsRune(filter.Query, '\x00') {
		return nil, ErrInvalidInput
	}
	if filter.CategoryID != "" && !ids.IsUUID(filter.CategoryID) {
		return nil, ErrInvalidID
	}
	if filter.Limit == 0 {
		filter.Limit = defaultPageSize
	}
	if filter.Limit < 1 || filter.Limit > maxPageSize || filter.Offset < 0 {
		return nil, ErrInvalidInput
	}
	orderBy, ok := map[string]string{"sort_order": "p.sort_order", "name": "lower(p.name)", "created_at": "p.created_at", "updated_at": "p.updated_at"}[filter.SortBy]
	if filter.SortBy == "" {
		orderBy = "p.sort_order"
		ok = true
	}
	if !ok {
		return nil, ErrInvalidInput
	}
	direction := strings.ToUpper(filter.SortDirection)
	if direction == "" {
		direction = "ASC"
	}
	if direction != "ASC" && direction != "DESC" {
		return nil, ErrInvalidInput
	}
	if filter.FavoritesOnly && userID == "" {
		return []Project{}, nil
	}
	query := `SELECT p.id, p.name, p.description, p.icon, COALESCE(p.category_id, ''), p.sort_order, p.created_at, p.updated_at, COALESCE(pref.value, 'false') = 'true'
		FROM projects p
		LEFT JOIN user_preferences pref ON pref.user_id = ? AND pref.preference_key = 'favorite:' || p.id
		WHERE 1 = 1`
	args := []any{userID}
	if filter.Query != "" {
		query += ` AND (lower(p.name) LIKE lower(?) ESCAPE '\' OR lower(p.description) LIKE lower(?) ESCAPE '\')`
		pattern := "%" + escapeLike(filter.Query) + "%"
		args = append(args, pattern, pattern)
	}
	if filter.CategoryID != "" {
		query += ` AND p.category_id = ?`
		args = append(args, filter.CategoryID)
	}
	if filter.FavoritesOnly {
		query += ` AND pref.value = 'true'`
	}
	query += ` ORDER BY ` + orderBy + ` ` + direction + `, lower(p.name), p.id LIMIT ? OFFSET ?`
	args = append(args, filter.Limit, filter.Offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	items := make([]Project, 0)
	for rows.Next() {
		var item Project
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Icon, &item.CategoryID, &item.SortOrder, &item.CreatedAt, &item.UpdatedAt, &item.Favorite); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan project: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate projects: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close project rows: %w", err)
	}
	for i := range items {
		items[i].Links, err = s.links(ctx, items[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (s *Service) Update(ctx context.Context, projectID string, input UpdateInput) (Project, error) {
	if !ids.IsUUID(projectID) {
		return Project{}, ErrInvalidID
	}
	input, err := normalize(input)
	if err != nil {
		return Project{}, err
	}
	if _, err := s.Get(ctx, projectID, ""); err != nil {
		return Project{}, err
	}
	if err := s.ensureNameAvailable(ctx, input.Name, projectID); err != nil {
		return Project{}, err
	}
	if err := s.ensureCategory(ctx, input.CategoryID); err != nil {
		return Project{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin project update: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE projects SET name = ?, description = ?, icon = ?, category_id = NULLIF(?, ''), sort_order = ?, updated_at = ? WHERE id = ?`, input.Name, input.Description, input.Icon, input.CategoryID, input.SortOrder, time.Now().Unix(), projectID)
	if err != nil {
		return Project{}, fmt.Errorf("update project: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Project{}, fmt.Errorf("check project update: %w", err)
	}
	if changed == 0 {
		return Project{}, ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_links WHERE project_id = ?`, projectID); err != nil {
		return Project{}, fmt.Errorf("replace project links: %w", err)
	}
	if err := insertLinks(ctx, tx, projectID, input.Links, time.Now().Unix()); err != nil {
		return Project{}, err
	}
	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit project update: %w", err)
	}
	return s.Get(ctx, projectID, "")
}

func (s *Service) Delete(ctx context.Context, projectID string) error {
	if !ids.IsUUID(projectID) {
		return ErrInvalidID
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin project deletion: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM user_preferences WHERE preference_key = ?`, "favorite:"+projectID); err != nil {
		return fmt.Errorf("delete project preferences: %w", err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, projectID)
	if err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check project deletion: %w", err)
	}
	if changed == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit project deletion: %w", err)
	}
	return nil
}

func (s *Service) SetFavorite(ctx context.Context, userID, projectID string, favorite bool) error {
	if !ids.IsUUID(userID) || !ids.IsUUID(projectID) {
		return ErrInvalidID
	}
	if err := s.ensureUser(ctx, userID); err != nil {
		return err
	}
	if _, err := s.get(ctx, projectID, ""); err != nil {
		return err
	}
	key := "favorite:" + projectID
	if !favorite {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM user_preferences WHERE user_id = ? AND preference_key = ?`, userID, key); err != nil {
			return fmt.Errorf("clear favorite: %w", err)
		}
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_preferences (user_id, preference_key, value, updated_at) VALUES (?, ?, 'true', ?) ON CONFLICT(user_id, preference_key) DO UPDATE SET value = 'true', updated_at = excluded.updated_at`, userID, key, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("set favorite: %w", err)
	}
	return nil
}

func (s *Service) get(ctx context.Context, projectID, userID string) (Project, error) {
	var item Project
	err := s.db.QueryRowContext(ctx, `SELECT p.id, p.name, p.description, p.icon, COALESCE(p.category_id, ''), p.sort_order, p.created_at, p.updated_at, COALESCE(pref.value, 'false') = 'true'
		FROM projects p LEFT JOIN user_preferences pref ON pref.user_id = ? AND pref.preference_key = 'favorite:' || p.id WHERE p.id = ?`, userID, projectID).
		Scan(&item.ID, &item.Name, &item.Description, &item.Icon, &item.CategoryID, &item.SortOrder, &item.CreatedAt, &item.UpdatedAt, &item.Favorite)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("get project: %w", err)
	}
	return item, nil
}

func (s *Service) links(ctx context.Context, projectID string) ([]Link, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, label, url, position, created_at FROM project_links WHERE project_id = ? ORDER BY position, id`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list project links: %w", err)
	}
	defer rows.Close()
	items := make([]Link, 0)
	for rows.Next() {
		var item Link
		if err := rows.Scan(&item.ID, &item.Label, &item.URL, &item.Position, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan project link: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate project links: %w", err)
	}
	return items, nil
}

func (s *Service) ensureNameAvailable(ctx context.Context, name, exceptID string) error {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM projects WHERE lower(name) = lower(?) AND id <> ?`, name, exceptID).Scan(&count)
	if err != nil {
		return fmt.Errorf("check project name: %w", err)
	}
	if count > 0 {
		return ErrConflict
	}
	return nil
}

func (s *Service) ensureCategory(ctx context.Context, categoryID string) error {
	if categoryID == "" {
		return nil
	}
	if !ids.IsUUID(categoryID) {
		return ErrInvalidID
	}
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM categories WHERE id = ?`, categoryID).Scan(&count); err != nil {
		return fmt.Errorf("check project category: %w", err)
	}
	if count == 0 {
		return ErrCategoryNotFound
	}
	return nil
}

func (s *Service) ensureUser(ctx context.Context, userID string) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE id = ? AND disabled = 0`, userID).Scan(&count); err != nil {
		return fmt.Errorf("check project user: %w", err)
	}
	if count == 0 {
		return ErrUserNotFound
	}
	return nil
}

func insertLinks(ctx context.Context, tx *sql.Tx, projectID string, links []LinkInput, createdAt int64) error {
	for position, link := range links {
		id, err := ids.NewUUID()
		if err != nil {
			return fmt.Errorf("generate project link ID: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO project_links (id, project_id, label, url, position, created_at) VALUES (?, ?, ?, ?, ?, ?)`, id, projectID, link.Label, link.URL, position, createdAt); err != nil {
			return fmt.Errorf("insert project link: %w", err)
		}
	}
	return nil
}

func normalize(input CreateInput) (CreateInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Icon = strings.TrimSpace(input.Icon)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 120 || strings.ContainsAny(input.Name, "\x00\r\n") {
		return CreateInput{}, ErrInvalidInput
	}
	if utf8.RuneCountInString(input.Description) > 2000 || strings.ContainsRune(input.Description, '\x00') || utf8.RuneCountInString(input.Icon) > 64 || strings.ContainsAny(input.Icon, "\x00\r\n") {
		return CreateInput{}, ErrInvalidInput
	}
	if input.SortOrder < -1000000 || input.SortOrder > 1000000 || len(input.Links) > maxProjectLinks {
		return CreateInput{}, ErrInvalidInput
	}
	if input.CategoryID != "" && !ids.IsUUID(input.CategoryID) {
		return CreateInput{}, ErrInvalidID
	}
	seen := make(map[string]struct{}, len(input.Links))
	for i := range input.Links {
		input.Links[i].Label = strings.TrimSpace(input.Links[i].Label)
		input.Links[i].URL = strings.TrimSpace(input.Links[i].URL)
		if err := validateLink(input.Links[i]); err != nil {
			return CreateInput{}, err
		}
		if _, ok := seen[input.Links[i].URL]; ok {
			return CreateInput{}, ErrInvalidInput
		}
		seen[input.Links[i].URL] = struct{}{}
	}
	return input, nil
}

func validateLink(link LinkInput) error {
	if link.Label == "" || utf8.RuneCountInString(link.Label) > 80 || strings.ContainsAny(link.Label, "\x00\r\n") || link.URL == "" || len(link.URL) > 2048 || strings.ContainsAny(link.URL, "\x00\r\n") {
		return ErrInvalidInput
	}
	parsed, err := url.Parse(link.URL)
	if err != nil || parsed.Opaque != "" || parsed.Hostname() == "" || parsed.User != nil && parsed.Scheme != "ssh" {
		return ErrInvalidInput
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "ssh":
	default:
		return ErrInvalidInput
	}
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			return ErrInvalidInput
		}
	}
	return nil
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
