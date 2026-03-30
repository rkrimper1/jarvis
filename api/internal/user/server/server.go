// Package server implements the gRPC UserService.
package server

import (
	"context"
	"log/slog"

	"go.opencensus.io/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "github.com/rkrimper1/jarvis/api/pb/user"
	"github.com/rkrimper1/jarvis/api/internal/user/store"
	"github.com/rkrimper1/jarvis/api/middleware"
)

// UserServer implements userv1.UserServiceServer.
type UserServer struct {
	userv1.UnimplementedUserServiceServer
	store *store.Store
	log   *slog.Logger
}

// New returns a UserServer backed by the given store.
func New(s *store.Store, log *slog.Logger) *UserServer {
	return &UserServer{store: s, log: log}
}

// ── Current user ──────────────────────────────────────────────────────────────

func (s *UserServer) storeRequired() error {
	if s.store == nil {
		return status.Error(codes.FailedPrecondition, "user store not configured (set USERS_DB_PATH)")
	}
	return nil
}

func (s *UserServer) GetMe(ctx context.Context, req *userv1.GetMeRequest) (*userv1.GetMeResponse, error) {
	if err := s.storeRequired(); err != nil { return nil, err }
	ctx, span := trace.StartSpan(ctx, "user/GetMe")
	defer span.End()
	middleware.AddRequestAttributes(ctx, "", req.Username)

	if req.Username == "" {
		return nil, status.Error(codes.InvalidArgument, "username is required")
	}
	u, err := s.store.GetByUsername(ctx, req.Username)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "user not found: %v", err)
	}
	u.PasswordHash = "" // never leak
	return &userv1.GetMeResponse{User: u}, nil
}

func (s *UserServer) UpdateProfile(ctx context.Context, req *userv1.UpdateProfileRequest) (*userv1.UpdateProfileResponse, error) {
	if err := s.storeRequired(); err != nil { return nil, err }
	ctx, span := trace.StartSpan(ctx, "user/UpdateProfile")
	defer span.End()
	middleware.AddRequestAttributes(ctx, "", req.Id)

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	u, err := s.store.UpdateProfile(ctx, req.Id, req.Email, req.DisplayName)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "update profile: %v", err)
	}
	u.PasswordHash = ""
	s.log.InfoContext(ctx, "UpdateProfile", slog.String("id", req.Id))
	return &userv1.UpdateProfileResponse{User: u}, nil
}

func (s *UserServer) ChangePassword(ctx context.Context, req *userv1.ChangePasswordRequest) (*userv1.ChangePasswordResponse, error) {
	if err := s.storeRequired(); err != nil { return nil, err }
	ctx, span := trace.StartSpan(ctx, "user/ChangePassword")
	defer span.End()
	middleware.AddRequestAttributes(ctx, "", req.Id)

	if req.Id == "" || req.CurrentPassword == "" || req.NewPassword == "" {
		return nil, status.Error(codes.InvalidArgument, "id, current_password, and new_password are required")
	}
	if err := s.store.ChangePassword(ctx, req.Id,
		[]byte(req.CurrentPassword), []byte(req.NewPassword)); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	s.log.InfoContext(ctx, "ChangePassword", slog.String("id", req.Id))
	return &userv1.ChangePasswordResponse{}, nil
}

// ── Admin CRUD ────────────────────────────────────────────────────────────────

func (s *UserServer) CreateUser(ctx context.Context, req *userv1.CreateUserRequest) (*userv1.CreateUserResponse, error) {
	if err := s.storeRequired(); err != nil { return nil, err }
	ctx, span := trace.StartSpan(ctx, "user/CreateUser")
	defer span.End()
	middleware.AddRequestAttributes(ctx, "", req.Username)

	if req.Username == "" || req.Password == "" {
		return nil, status.Error(codes.InvalidArgument, "username and password are required")
	}
	role := req.Role
	if role == userv1.Role_ROLE_UNSPECIFIED {
		role = userv1.Role_ROLE_VIEWER
	}
	u, err := s.store.Create(ctx, req.Username, req.Email, req.DisplayName, req.Password, role)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create user: %v", err)
	}
	u.PasswordHash = ""
	s.log.InfoContext(ctx, "CreateUser", slog.String("username", req.Username), slog.String("role", role.String()))
	return &userv1.CreateUserResponse{User: u}, nil
}

func (s *UserServer) GetUser(ctx context.Context, req *userv1.GetUserRequest) (*userv1.GetUserResponse, error) {
	if err := s.storeRequired(); err != nil { return nil, err }
	ctx, span := trace.StartSpan(ctx, "user/GetUser")
	defer span.End()
	middleware.AddRequestAttributes(ctx, "", req.GetId())

	var u *userv1.User
	var err error
	switch v := req.Lookup.(type) {
	case *userv1.GetUserRequest_Id:
		u, err = s.store.GetByID(ctx, v.Id)
	case *userv1.GetUserRequest_Username:
		u, err = s.store.GetByUsername(ctx, v.Username)
	default:
		return nil, status.Error(codes.InvalidArgument, "provide id, username, or email in lookup")
	}
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	u.PasswordHash = ""
	return &userv1.GetUserResponse{User: u}, nil
}

func (s *UserServer) ListUsers(ctx context.Context, req *userv1.ListUsersRequest) (*userv1.ListUsersResponse, error) {
	if err := s.storeRequired(); err != nil { return nil, err }
	ctx, span := trace.StartSpan(ctx, "user/ListUsers")
	defer span.End()
	middleware.AddRequestAttributes(ctx, "", "")

	users, err := s.store.List(ctx, req.Role, req.ActiveOnly)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list users: %v", err)
	}
	for _, u := range users {
		u.PasswordHash = ""
	}
	return &userv1.ListUsersResponse{
		Users:      users,
		TotalCount: int32(len(users)),
	}, nil
}

func (s *UserServer) UpdateUser(ctx context.Context, req *userv1.UpdateUserRequest) (*userv1.UpdateUserResponse, error) {
	if err := s.storeRequired(); err != nil { return nil, err }
	ctx, span := trace.StartSpan(ctx, "user/UpdateUser")
	defer span.End()
	middleware.AddRequestAttributes(ctx, "", req.Id)

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	u, err := s.store.UpdateUser(ctx, req.Id, req.Role, req.IsActive)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "update user: %v", err)
	}
	u.PasswordHash = ""
	s.log.InfoContext(ctx, "UpdateUser", slog.String("id", req.Id))
	return &userv1.UpdateUserResponse{User: u}, nil
}

func (s *UserServer) DeleteUser(ctx context.Context, req *userv1.DeleteUserRequest) (*userv1.DeleteUserResponse, error) {
	if err := s.storeRequired(); err != nil { return nil, err }
	ctx, span := trace.StartSpan(ctx, "user/DeleteUser")
	defer span.End()
	middleware.AddRequestAttributes(ctx, "", req.Id)

	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if err := s.store.Delete(ctx, req.Id); err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	s.log.InfoContext(ctx, "DeleteUser", slog.String("id", req.Id))
	return &userv1.DeleteUserResponse{Id: req.Id}, nil
}
