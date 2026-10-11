package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/projecthub/projecthub/internal/ids"
	"github.com/projecthub/projecthub/internal/rbac"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInvalidSession     = errors.New("invalid or expired session")
	ErrBootstrapComplete  = errors.New("administrator bootstrap already completed")
	ErrWeakPassword       = errors.New("password must contain 12 to 1024 bytes")
	ErrInvalidUsername    = errors.New("username must be 3 to 64 ASCII letters, digits, dots, underscores, or hyphens")
	usernamePattern       = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{2,63}$`)
)

const sessionLifetime = 12 * time.Hour

type Service struct {
	db  *sql.DB
	now func() time.Time
}

type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Role      rbac.Role `json:"role"`
	Disabled  bool      `json:"disabled"`
	CreatedAt int64     `json:"createdAt"`
}
type Session struct {
	ID        string
	Token     string
	CSRFToken string
	ExpiresAt int64
}
type Principal struct {
	User      User
	SessionID string
	CSRFToken string
}

func NewService(db *sql.DB) *Service { return &Service{db: db, now: time.Now} }

func (s *Service) BootstrapAdmin(ctx context.Context, username, password string) (User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !usernamePattern.MatchString(username) {
		return User{}, ErrInvalidUsername
	}
	if err := validatePassword(password); err != nil {
		return User{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return User{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return User{}, fmt.Errorf("begin bootstrap: %w", err)
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&count); err != nil {
		return User{}, fmt.Errorf("check bootstrap state: %w", err)
	}
	if count != 0 {
		return User{}, ErrBootstrapComplete
	}
	id, err := ids.NewUUID()
	if err != nil {
		return User{}, err
	}
	now := s.now().Unix()
	if _, err = tx.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,role,disabled,created_at,updated_at) VALUES(?,?,?,'admin',0,?,?)`, id, username, hash, now, now); err != nil {
		return User{}, fmt.Errorf("create administrator: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return User{}, fmt.Errorf("commit bootstrap: %w", err)
	}
	return User{ID: id, Username: username, Role: rbac.RoleAdmin, CreatedAt: now}, nil
}

func (s *Service) Authenticate(ctx context.Context, username, password string) (User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	var u User
	var hash string
	var disabled int
	err := s.db.QueryRowContext(ctx, `SELECT id,username,password_hash,role,disabled,created_at FROM users WHERE username=?`, username).Scan(&u.ID, &u.Username, &hash, &u.Role, &disabled, &u.CreatedAt)
	if err != nil { // Make unknown usernames pay the normal password-hash cost.
		dummy, _ := hashPassword("not-a-real-password-value")
		_ = verifyPassword(dummy, password)
		return User{}, ErrInvalidCredentials
	}
	if disabled != 0 || !verifyPassword(hash, password) {
		return User{}, ErrInvalidCredentials
	}
	return u, nil
}

func (s *Service) NewSession(ctx context.Context, userID, previousToken string) (Session, error) {
	var session Session
	var err error
	if session.ID, err = ids.NewUUID(); err != nil {
		return session, err
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return session, err
	}
	session.Token = base64.RawURLEncoding.EncodeToString(raw)
	csrf := make([]byte, 32)
	if _, err = rand.Read(csrf); err != nil {
		return session, err
	}
	session.CSRFToken = base64.RawURLEncoding.EncodeToString(csrf)
	now := s.now().Unix()
	session.ExpiresAt = s.now().Add(sessionLifetime).Unix()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return Session{}, err
	}
	defer tx.Rollback()
	if previousToken != "" {
		_, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash(previousToken))
		if err != nil {
			return Session{}, err
		}
	}
	var disabled int
	if err = tx.QueryRowContext(ctx, `SELECT disabled FROM users WHERE id=?`, userID).Scan(&disabled); err != nil || disabled != 0 {
		return Session{}, ErrInvalidSession
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sessions(id,user_id,token_hash,csrf_token,expires_at,created_at,last_seen_at) VALUES(?,?,?,?,?,?,?)`, session.ID, userID, tokenHash(session.Token), session.CSRFToken, session.ExpiresAt, now, now)
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *Service) ResolveSession(ctx context.Context, token string) (Principal, error) {
	if token == "" {
		return Principal{}, ErrInvalidSession
	}
	now := s.now().Unix()
	var p Principal
	var disabled int
	var expires int64
	err := s.db.QueryRowContext(ctx, `SELECT u.id,u.username,u.role,u.disabled,u.created_at,s.id,s.csrf_token,s.expires_at FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=?`, tokenHash(token)).Scan(&p.User.ID, &p.User.Username, &p.User.Role, &disabled, &p.User.CreatedAt, &p.SessionID, &p.CSRFToken, &expires)
	if err != nil || disabled != 0 {
		return Principal{}, ErrInvalidSession
	}
	if expires <= now {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, p.SessionID)
		return Principal{}, ErrInvalidSession
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE sessions SET last_seen_at=? WHERE id=?`, now, p.SessionID)
	return p, nil
}

func (s *Service) RevokeSession(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash=?`, tokenHash(token))
	return err
}
func (s *Service) RevokeUserSessions(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, userID)
	return err
}
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
