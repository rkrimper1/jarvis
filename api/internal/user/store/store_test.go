package store_test

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/rkrimper1/jarvis/api/internal/user/store"
	userv1 "github.com/rkrimper1/jarvis/api/pb/user"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	s, err := store.New(db, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	return s
}

func TestCreate_And_GetByUsername(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.Create(ctx, "pepper-potts", "pepper@stark.industries", "Pepper Potts", "secret", userv1.Role_ROLE_EDITOR)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if u.Id == "" {
		t.Fatal("expected non-empty id")
	}
	if u.Username != "pepper-potts" {
		t.Errorf("username = %q, want %q", u.Username, "pepper-potts")
	}
	if u.Role != userv1.Role_ROLE_EDITOR {
		t.Errorf("role = %v, want ROLE_EDITOR", u.Role)
	}
	if !u.IsActive {
		t.Error("expected is_active = true")
	}

	got, err := s.GetByUsername(ctx, "pepper-potts")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if got.Id != u.Id {
		t.Errorf("id mismatch: got %q, want %q", got.Id, u.Id)
	}
}

func TestCreate_DuplicateUsername(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.Create(ctx, "happy-hogan", "", "", "pass1", userv1.Role_ROLE_VIEWER); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := s.Create(ctx, "happy-hogan", "", "", "pass2", userv1.Role_ROLE_VIEWER); err == nil {
		t.Fatal("expected error on duplicate username, got nil")
	}
}

func TestGetByID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	created, err := s.Create(ctx, "nick-fury", "", "Nick Fury", "shield", userv1.Role_ROLE_ADMIN)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.GetByID(ctx, created.Id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Username != "nick-fury" {
		t.Errorf("username = %q, want %q", got.Username, "nick-fury")
	}
}

func TestGetByID_NotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.GetByID(context.Background(), "nonexistent-uuid"); err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

func TestCheckPassword_Success(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.Create(ctx, "james-rhodes", "", "", "war-machine", userv1.Role_ROLE_EDITOR); err != nil {
		t.Fatalf("Create: %v", err)
	}
	u, err := s.CheckPassword(ctx, "james-rhodes", []byte("war-machine"))
	if err != nil {
		t.Fatalf("CheckPassword: %v", err)
	}
	if u.Username != "james-rhodes" {
		t.Errorf("username = %q, want %q", u.Username, "james-rhodes")
	}
}

func TestCheckPassword_WrongPassword(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.Create(ctx, "james-rhodes", "", "", "war-machine", userv1.Role_ROLE_EDITOR); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.CheckPassword(ctx, "james-rhodes", []byte("wrong")); err == nil {
		t.Fatal("expected error for wrong password, got nil")
	}
}

func TestCheckPassword_UnknownUser(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CheckPassword(context.Background(), "ghost", []byte("pass")); err == nil {
		t.Fatal("expected error for unknown user, got nil")
	}
}

func TestList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, u := range []struct {
		name string
		role userv1.Role
	}{
		{"user-a", userv1.Role_ROLE_VIEWER},
		{"user-b", userv1.Role_ROLE_EDITOR},
		{"user-c", userv1.Role_ROLE_ADMIN},
	} {
		if _, err := s.Create(ctx, u.name, "", "", "pass", u.role); err != nil {
			t.Fatalf("Create %s: %v", u.name, err)
		}
	}

	// List all (store starts empty — seed only runs when DB is initially empty
	// and that's handled by New; our test store calls seed which reads SEED_* envs
	// defaulting to tony-stark, so count includes that seed user)
	all, err := s.List(ctx, userv1.Role_ROLE_UNSPECIFIED, false)
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	// 3 created + 1 seeded (tony-stark default)
	if len(all) < 3 {
		t.Errorf("expected at least 3 users, got %d", len(all))
	}

	// Filter by ROLE_VIEWER
	viewers, err := s.List(ctx, userv1.Role_ROLE_VIEWER, false)
	if err != nil {
		t.Fatalf("List viewers: %v", err)
	}
	for _, u := range viewers {
		if u.Role != userv1.Role_ROLE_VIEWER {
			t.Errorf("expected ROLE_VIEWER, got %v", u.Role)
		}
	}
}

func TestUpdateProfile(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.Create(ctx, "rhodey", "", "", "pass", userv1.Role_ROLE_VIEWER)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := s.UpdateProfile(ctx, u.Id, "rhodey@military.gov", "James Rhodes")
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if updated.Email != "rhodey@military.gov" {
		t.Errorf("email = %q, want %q", updated.Email, "rhodey@military.gov")
	}
	if updated.DisplayName != "James Rhodes" {
		t.Errorf("display_name = %q, want %q", updated.DisplayName, "James Rhodes")
	}
}

func TestUpdateUser_RoleChange(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.Create(ctx, "loki", "", "", "pass", userv1.Role_ROLE_VIEWER)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	updated, err := s.UpdateUser(ctx, u.Id, userv1.Role_ROLE_EDITOR, true)
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if updated.Role != userv1.Role_ROLE_EDITOR {
		t.Errorf("role = %v, want ROLE_EDITOR", updated.Role)
	}
}

func TestChangePassword(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.Create(ctx, "bruce-banner", "", "", "hulk-smash", userv1.Role_ROLE_VIEWER)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := s.ChangePassword(ctx, u.Id, []byte("hulk-smash"), []byte("calm-bruce")); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	// Old password should no longer work
	if _, err := s.CheckPassword(ctx, "bruce-banner", []byte("hulk-smash")); err == nil {
		t.Fatal("expected old password to be invalid after change")
	}
	// New password should work
	if _, err := s.CheckPassword(ctx, "bruce-banner", []byte("calm-bruce")); err != nil {
		t.Fatalf("new password should be valid: %v", err)
	}
}

func TestChangePassword_WrongCurrent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.Create(ctx, "natasha", "", "", "black-widow", userv1.Role_ROLE_VIEWER)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.ChangePassword(ctx, u.Id, []byte("wrong"), []byte("new")); err == nil {
		t.Fatal("expected error for wrong current password, got nil")
	}
}

func TestDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	u, err := s.Create(ctx, "obadiah-stane", "", "", "ironmonger", userv1.Role_ROLE_VIEWER)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Delete(ctx, u.Id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.GetByID(ctx, u.Id); err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestDelete_NotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.Delete(context.Background(), "does-not-exist"); err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}
