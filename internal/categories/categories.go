package categories

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/projecthub/projecthub/internal/ids"
)

var (
	ErrInvalidID    = errors.New("invalid category ID")
	ErrInvalidInput = errors.New("invalid category input")
	ErrNotFound     = errors.New("category not found")
	ErrConflict     = errors.New("category already exists")
)

type Category struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Icon      string `json:"icon"`
	SortOrder int    `json:"sort_order"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type CreateInput struct {
	Name      string
	Icon      string
	SortOrder int
}

type UpdateInput = CreateInput

type Service struct{ db *sql.DB }

func NewService(db *sql.DB) *Service { return &Service{db: db} }

func (s *Service) Create(ctx context.Context, input CreateInput) (Category, error) {
	input, err := normalize(input)
	if err != nil {
		return Category{}, err
	}
	if err := s.ensureNameAvailable(ctx, input.Name, ""); err != nil {
		return Category{}, err
	}
	id, err := ids.NewUUID()
	if err != nil {
		return Category{}, fmt.Errorf("generate category ID: %w", err)
	}
	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx, `INSERT INTO categories (id, name, icon, sort_order, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, id, input.Name, input.Icon, input.SortOrder, now, now)
	if err != nil {
		return Category{}, fmt.Errorf("insert category: %w", err)
	}
	return s.Get(ctx, id)
}

func (s *Service) Get(ctx context.Context, id string) (Category, error) {
	if !ids.IsUUID(id) {
		return Category{}, ErrInvalidID
	}
	var item Category
	err := s.db.QueryRowContext(ctx, `SELECT id, name, icon, sort_order, created_at, updated_at FROM categories WHERE id = ?`, id).Scan(&item.ID, &item.Name, &item.Icon, &item.SortOrder, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Category{}, ErrNotFound
	}
	if err != nil {
		return Category{}, fmt.Errorf("get category: %w", err)
	}
	return item, nil
}

func (s *Service) List(ctx context.Context) ([]Category, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, icon, sort_order, created_at, updated_at FROM categories ORDER BY sort_order, lower(name), id`)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()
	items := make([]Category, 0)
	for rows.Next() {
		var item Category
		if err := rows.Scan(&item.ID, &item.Name, &item.Icon, &item.SortOrder, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan category: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate categories: %w", err)
	}
	return items, nil
}

func (s *Service) Update(ctx context.Context, id string, input UpdateInput) (Category, error) {
	if !ids.IsUUID(id) {
		return Category{}, ErrInvalidID
	}
	input, err := normalize(input)
	if err != nil {
		return Category{}, err
	}
	if _, err := s.Get(ctx, id); err != nil {
		return Category{}, err
	}
	if err := s.ensureNameAvailable(ctx, input.Name, id); err != nil {
		return Category{}, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE categories SET name = ?, icon = ?, sort_order = ?, updated_at = ? WHERE id = ?`, input.Name, input.Icon, input.SortOrder, time.Now().Unix(), id)
	if err != nil {
		return Category{}, fmt.Errorf("update category: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Category{}, fmt.Errorf("check category update: %w", err)
	}
	if changed == 0 {
		return Category{}, ErrNotFound
	}
	return s.Get(ctx, id)
}

func (s *Service) Delete(ctx context.Context, id string) error {
	if !ids.IsUUID(id) {
		return ErrInvalidID
	}
	result, err := s.db.ExecContext(ctx, `DELETE FROM categories WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete category: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check category deletion: %w", err)
	}
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Service) ensureNameAvailable(ctx context.Context, name, exceptID string) error {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM categories WHERE lower(name) = lower(?) AND id <> ?`, name, exceptID).Scan(&count)
	if err != nil {
		return fmt.Errorf("check category name: %w", err)
	}
	if count > 0 {
		return ErrConflict
	}
	return nil
}

func normalize(input CreateInput) (CreateInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Icon = strings.TrimSpace(input.Icon)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > 80 || strings.ContainsAny(input.Name, "\x00\r\n") {
		return CreateInput{}, ErrInvalidInput
	}
	if utf8.RuneCountInString(input.Icon) > 64 || strings.ContainsAny(input.Icon, "\x00\r\n") {
		return CreateInput{}, ErrInvalidInput
	}
	return input, nil
}
