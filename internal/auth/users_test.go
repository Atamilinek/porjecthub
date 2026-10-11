package auth_test

import (
	"errors"
	"testing"

	"github.com/projecthub/projecthub/internal/auth"
	"github.com/projecthub/projecthub/internal/rbac"
)

func TestManagedUsersAreHashedRevocableAndProtectLastAdministrator(t *testing.T) {
	db := testDatabase(t)
	service := auth.NewService(db)
	admin, err := service.BootstrapAdmin(t.Context(), "root-admin", "correct-horse-battery-staple")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.SetUserDisabled(t.Context(), admin.ID, true); !errors.Is(err, auth.ErrLastAdministrator) {
		t.Fatalf("last admin disable error=%v", err)
	}
	created, err := service.CreateUser(t.Context(), " Support.User ", "another-correct-horse", rbac.RoleOperator)
	if err != nil || created.Username != "support.user" {
		t.Fatalf("created=%+v err=%v", created, err)
	}
	var hash string
	if err = db.QueryRow(`SELECT password_hash FROM users WHERE id=?`, created.ID).Scan(&hash); err != nil || hash == "another-correct-horse" {
		t.Fatalf("stored password hash invalid: %v", err)
	}
	if _, err = service.Authenticate(t.Context(), created.Username, "another-correct-horse"); err != nil {
		t.Fatalf("new user authentication failed: %v", err)
	}
	session, err := service.NewSession(t.Context(), created.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.ResetUserPassword(t.Context(), created.ID, "fresh-secure-password"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ResolveSession(t.Context(), session.Token); !errors.Is(err, auth.ErrInvalidSession) {
		t.Fatalf("password reset retained old session: %v", err)
	}
	if err = service.SetUserDisabled(t.Context(), created.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Authenticate(t.Context(), created.Username, "fresh-secure-password"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("disabled account authenticated: %v", err)
	}
	if _, err = service.CreateUser(t.Context(), "invalid-role", "another-correct-horse", rbac.Role("superuser")); !errors.Is(err, auth.ErrInvalidRole) {
		t.Fatalf("invalid role error=%v", err)
	}
}
