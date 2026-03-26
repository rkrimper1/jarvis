package server_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userserver "github.com/rkrimper1/jarvis/api/internal/user/server"
	"github.com/rkrimper1/jarvis/api/internal/user/store"
	userv1 "github.com/rkrimper1/jarvis/api/pb/user"
)

// newTestServer returns a UserServer backed by a fresh in-memory SQLite store.
func newTestServer(t *testing.T) *userserver.UserServer {
	t.Helper()
	f, err := os.CreateTemp("", "users-srv-test-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s, err := store.New(f.Name(), log)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return userserver.New(s, log)
}

// nilServer has no store — tests FailedPrecondition guards.
func nilServer() *userserver.UserServer {
	return userserver.New(nil, slog.New(slog.NewTextHandler(os.Stderr, nil)))
}

// ── nil store guard ───────────────────────────────────────────────────────────

func TestGetMe_NilStore(t *testing.T) {
	_, err := nilServer().GetMe(context.Background(), &userv1.GetMeRequest{Username: "tony-stark"})
	if code := status.Code(err); code != codes.FailedPrecondition {
		t.Errorf("code = %v, want FailedPrecondition", code)
	}
}

func TestCreateUser_NilStore(t *testing.T) {
	_, err := nilServer().CreateUser(context.Background(), &userv1.CreateUserRequest{
		Username: "x", Password: "y",
	})
	if code := status.Code(err); code != codes.FailedPrecondition {
		t.Errorf("code = %v, want FailedPrecondition", code)
	}
}

// ── GetMe ─────────────────────────────────────────────────────────────────────

func TestGetMe_EmptyUsername(t *testing.T) {
	srv := newTestServer(t)
	_, err := srv.GetMe(context.Background(), &userv1.GetMeRequest{})
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", code)
	}
}

func TestGetMe_NotFound(t *testing.T) {
	srv := newTestServer(t)
	_, err := srv.GetMe(context.Background(), &userv1.GetMeRequest{Username: "nobody"})
	if code := status.Code(err); code != codes.NotFound {
		t.Errorf("code = %v, want NotFound", code)
	}
}

func TestGetMe_Success(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	created, err := srv.CreateUser(ctx, &userv1.CreateUserRequest{
		Username: "pepper-potts", Password: "ceo", Role: userv1.Role_ROLE_EDITOR,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	got, err := srv.GetMe(ctx, &userv1.GetMeRequest{Username: "pepper-potts"})
	if err != nil {
		t.Fatalf("GetMe: %v", err)
	}
	if got.User.Id != created.User.Id {
		t.Errorf("id mismatch: got %q, want %q", got.User.Id, created.User.Id)
	}
	if got.User.PasswordHash != "" {
		t.Error("PasswordHash must be empty in response")
	}
}

// ── CreateUser ────────────────────────────────────────────────────────────────

func TestCreateUser_MissingFields(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	// missing username
	if _, err := srv.CreateUser(ctx, &userv1.CreateUserRequest{Password: "p"}); err == nil {
		t.Error("expected error for missing username")
	}
	// missing password
	if _, err := srv.CreateUser(ctx, &userv1.CreateUserRequest{Username: "u"}); err == nil {
		t.Error("expected error for missing password")
	}
}

func TestCreateUser_DefaultsToViewer(t *testing.T) {
	srv := newTestServer(t)
	res, err := srv.CreateUser(context.Background(), &userv1.CreateUserRequest{
		Username: "intern", Password: "pass",
		// Role intentionally omitted (ROLE_UNSPECIFIED)
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if res.User.Role != userv1.Role_ROLE_VIEWER {
		t.Errorf("role = %v, want ROLE_VIEWER", res.User.Role)
	}
}

func TestCreateUser_PasswordHashNotReturned(t *testing.T) {
	srv := newTestServer(t)
	res, err := srv.CreateUser(context.Background(), &userv1.CreateUserRequest{
		Username: "happy-hogan", Password: "bodyguard", Role: userv1.Role_ROLE_VIEWER,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if res.User.PasswordHash != "" {
		t.Error("PasswordHash must be stripped from response")
	}
}

// ── ListUsers ─────────────────────────────────────────────────────────────────

func TestListUsers(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	for _, name := range []string{"user-1", "user-2", "user-3"} {
		if _, err := srv.CreateUser(ctx, &userv1.CreateUserRequest{
			Username: name, Password: "pass", Role: userv1.Role_ROLE_VIEWER,
		}); err != nil {
			t.Fatalf("CreateUser %s: %v", name, err)
		}
	}

	res, err := srv.ListUsers(ctx, &userv1.ListUsersRequest{})
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	// 3 created + seed user (tony-stark default from store.New)
	if int(res.TotalCount) != len(res.Users) {
		t.Errorf("TotalCount %d != len(Users) %d", res.TotalCount, len(res.Users))
	}
	if len(res.Users) < 3 {
		t.Errorf("expected at least 3 users, got %d", len(res.Users))
	}
	for _, u := range res.Users {
		if u.PasswordHash != "" {
			t.Errorf("user %s has non-empty PasswordHash in list response", u.Username)
		}
	}
}

// ── UpdateProfile ─────────────────────────────────────────────────────────────

func TestUpdateProfile_MissingID(t *testing.T) {
	srv := newTestServer(t)
	_, err := srv.UpdateProfile(context.Background(), &userv1.UpdateProfileRequest{})
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", code)
	}
}

func TestUpdateProfile_Success(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	created, err := srv.CreateUser(ctx, &userv1.CreateUserRequest{
		Username: "rhodey", Password: "pass", Role: userv1.Role_ROLE_VIEWER,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	res, err := srv.UpdateProfile(ctx, &userv1.UpdateProfileRequest{
		Id: created.User.Id, Email: "rhodey@af.mil", DisplayName: "James Rhodes",
	})
	if err != nil {
		t.Fatalf("UpdateProfile: %v", err)
	}
	if res.User.Email != "rhodey@af.mil" {
		t.Errorf("email = %q, want %q", res.User.Email, "rhodey@af.mil")
	}
	if res.User.DisplayName != "James Rhodes" {
		t.Errorf("display_name = %q, want %q", res.User.DisplayName, "James Rhodes")
	}
}

// ── ChangePassword ────────────────────────────────────────────────────────────

func TestChangePassword_MissingFields(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	cases := []*userv1.ChangePasswordRequest{
		{CurrentPassword: "old", NewPassword: "new"},           // missing id
		{Id: "x", NewPassword: "new"},                          // missing current
		{Id: "x", CurrentPassword: "old"},                      // missing new
	}
	for _, req := range cases {
		if _, err := srv.ChangePassword(ctx, req); err == nil {
			t.Errorf("expected error for %+v, got nil", req)
		}
	}
}

func TestChangePassword_Success(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	created, err := srv.CreateUser(ctx, &userv1.CreateUserRequest{
		Username: "bruce-banner", Password: "hulk-smash", Role: userv1.Role_ROLE_VIEWER,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if _, err := srv.ChangePassword(ctx, &userv1.ChangePasswordRequest{
		Id: created.User.Id, CurrentPassword: "hulk-smash", NewPassword: "calm-bruce",
	}); err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}
}

func TestChangePassword_WrongCurrent(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	created, err := srv.CreateUser(ctx, &userv1.CreateUserRequest{
		Username: "wanda", Password: "scarlet-witch", Role: userv1.Role_ROLE_VIEWER,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, err = srv.ChangePassword(ctx, &userv1.ChangePasswordRequest{
		Id: created.User.Id, CurrentPassword: "wrong", NewPassword: "new",
	})
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", code)
	}
}

// ── DeleteUser ────────────────────────────────────────────────────────────────

func TestDeleteUser_MissingID(t *testing.T) {
	srv := newTestServer(t)
	_, err := srv.DeleteUser(context.Background(), &userv1.DeleteUserRequest{})
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", code)
	}
}

func TestDeleteUser_Success(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	created, err := srv.CreateUser(ctx, &userv1.CreateUserRequest{
		Username: "obadiah-stane", Password: "ironmonger", Role: userv1.Role_ROLE_VIEWER,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	res, err := srv.DeleteUser(ctx, &userv1.DeleteUserRequest{Id: created.User.Id})
	if err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if res.Id != created.User.Id {
		t.Errorf("deleted id = %q, want %q", res.Id, created.User.Id)
	}

	// Confirm gone
	_, err = srv.GetMe(ctx, &userv1.GetMeRequest{Username: "obadiah-stane"})
	if code := status.Code(err); code != codes.NotFound {
		t.Errorf("after delete: code = %v, want NotFound", code)
	}
}

func TestDeleteUser_NotFound(t *testing.T) {
	srv := newTestServer(t)
	_, err := srv.DeleteUser(context.Background(), &userv1.DeleteUserRequest{Id: "no-such-uuid"})
	if code := status.Code(err); code != codes.NotFound {
		t.Errorf("code = %v, want NotFound", code)
	}
}

// ── GetUser ───────────────────────────────────────────────────────────────────

func TestGetUser_ByID(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	created, err := srv.CreateUser(ctx, &userv1.CreateUserRequest{
		Username: "nick-fury", Password: "shield", Role: userv1.Role_ROLE_ADMIN,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	got, err := srv.GetUser(ctx, &userv1.GetUserRequest{
		Lookup: &userv1.GetUserRequest_Id{Id: created.User.Id},
	})
	if err != nil {
		t.Fatalf("GetUser by id: %v", err)
	}
	if got.User.Username != "nick-fury" {
		t.Errorf("username = %q, want %q", got.User.Username, "nick-fury")
	}
}

func TestGetUser_ByUsername(t *testing.T) {
	srv := newTestServer(t)
	ctx := context.Background()

	if _, err := srv.CreateUser(ctx, &userv1.CreateUserRequest{
		Username: "natasha", Password: "spy", Role: userv1.Role_ROLE_EDITOR,
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	got, err := srv.GetUser(ctx, &userv1.GetUserRequest{
		Lookup: &userv1.GetUserRequest_Username{Username: "natasha"},
	})
	if err != nil {
		t.Fatalf("GetUser by username: %v", err)
	}
	if got.User.Role != userv1.Role_ROLE_EDITOR {
		t.Errorf("role = %v, want ROLE_EDITOR", got.User.Role)
	}
}

func TestGetUser_NoLookup(t *testing.T) {
	srv := newTestServer(t)
	_, err := srv.GetUser(context.Background(), &userv1.GetUserRequest{})
	if code := status.Code(err); code != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", code)
	}
}
