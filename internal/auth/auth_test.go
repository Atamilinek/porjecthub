package auth_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projecthub/projecthub/internal/auth"
	"github.com/projecthub/projecthub/internal/migrate"
	"github.com/projecthub/projecthub/internal/store"
	"github.com/projecthub/projecthub/migrations"
)

func TestBootstrapAdminIsOneTimeAndUsesArgon2id(t *testing.T) {
	db := testDatabase(t)
	service := auth.NewService(db)
	ctx := context.Background()
	password := "correct-horse-battery-staple"
	admin, err := service.BootstrapAdmin(ctx, "ProjectAdmin", password)
	if err != nil {
		t.Fatalf("BootstrapAdmin() error = %v", err)
	}
	if admin.Role != "admin" || admin.Username != "projectadmin" {
		t.Fatalf("bootstrapped admin = %#v, want normalized admin account", admin)
	}
	var passwordHash string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, admin.ID).Scan(&passwordHash); err != nil {
		t.Fatalf("read stored password hash: %v", err)
	}
	if !strings.HasPrefix(passwordHash, "$argon2id$v=19$m=19456,t=2,p=1$") || passwordHash == password {
		t.Fatalf("stored password hash is not the expected Argon2id encoding: %q", passwordHash)
	}
	if _, err := service.Authenticate(ctx, "projectadmin", password); err != nil {
		t.Fatalf("Authenticate(correct password) error = %v", err)
	}
	if _, err := service.Authenticate(ctx, "projectadmin", "wrong-password"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("Authenticate(wrong password) error = %v, want ErrInvalidCredentials", err)
	}
	if _, err := service.BootstrapAdmin(ctx, "secondadmin", password); !errors.Is(err, auth.ErrBootstrapComplete) {
		t.Fatalf("second BootstrapAdmin() error = %v, want ErrBootstrapComplete", err)
	}
}

func TestSessionsRotateExpireAndRevoke(t *testing.T) {
	db := testDatabase(t)
	service := auth.NewService(db)
	ctx := context.Background()
	admin, err := service.BootstrapAdmin(ctx, "operator", "correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.NewSession(ctx, admin.ID, "")
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if first.Token == "" || first.CSRFToken == "" || first.ExpiresAt <= 0 {
		t.Fatalf("new session is missing token, CSRF token, or expiry: %#v", first)
	}
	if _, err := service.ResolveSession(ctx, first.Token); err != nil {
		t.Fatalf("ResolveSession(first token) error = %v", err)
	}
	second, err := service.NewSession(ctx, admin.ID, first.Token)
	if err != nil {
		t.Fatalf("rotate session: %v", err)
	}
	if _, err := service.ResolveSession(ctx, first.Token); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatalf("old session after rotation error = %v, want ErrInvalidSession", err)
	}
	if _, err := service.ResolveSession(ctx, second.Token); err != nil {
		t.Fatalf("ResolveSession(rotated token) error = %v", err)
	}
	if err := service.RevokeSession(ctx, second.Token); err != nil {
		t.Fatalf("RevokeSession() error = %v", err)
	}
	if _, err := service.ResolveSession(ctx, second.Token); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatalf("revoked session error = %v, want ErrInvalidSession", err)
	}
	third, err := service.NewSession(ctx, admin.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE sessions SET expires_at = 1 WHERE id = ?`, third.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResolveSession(ctx, third.Token); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatalf("expired session error = %v, want ErrInvalidSession", err)
	}
	var remaining int
	if err := db.QueryRow(`SELECT count(*) FROM sessions WHERE id = ?`, third.ID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("expired session records retained = %d, want 0", remaining)
	}
}

func TestBootstrapRejectsWeakPasswordsAndInvalidUsernames(t *testing.T) {
	service := auth.NewService(testDatabase(t))
	if _, err := service.BootstrapAdmin(context.Background(), "admin", "short"); !errors.Is(err, auth.ErrWeakPassword) {
		t.Fatalf("BootstrapAdmin(weak password) error = %v, want ErrWeakPassword", err)
	}
	if _, err := service.BootstrapAdmin(context.Background(), "admin name", "correct-horse-battery-staple"); !errors.Is(err, auth.ErrInvalidUsername) {
		t.Fatalf("BootstrapAdmin(invalid username) error = %v, want ErrInvalidUsername", err)
	}
}

func testDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	if err := migrate.Apply(t.Context(), db, migrations.FS); err != nil {
		db.Close()
		t.Fatalf("migrate.Apply() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
