// Package store provides the SQLite-backed user store with bcrypt password hashing.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/protobuf/types/known/timestamppb"

	userv1 "github.com/rkrimper1/jarvis/api/pb/user"
)

const schema = `
CREATE TABLE IF NOT EXISTS users (
    id           TEXT PRIMARY KEY,
    username     TEXT NOT NULL UNIQUE,
    email        TEXT NOT NULL DEFAULT '',
    display_name TEXT NOT NULL DEFAULT '',
    role         TEXT NOT NULL DEFAULT 'ROLE_VIEWER'
                 CHECK(role IN ('ROLE_ADMIN','ROLE_EDITOR','ROLE_VIEWER')),
    password_hash TEXT NOT NULL,
    is_active    INTEGER NOT NULL DEFAULT 1,
    created_at   DATETIME DEFAULT (datetime('now')),
    updated_at   DATETIME DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
`

// Store is the SQLite-backed user store.
type Store struct {
	db  *sql.DB
	log *slog.Logger
}

// New applies the schema to the shared db and seeds initial users if the
// table is empty. The caller owns db and is responsible for closing it.
func New(db *sql.DB, log *slog.Logger) (*Store, error) {
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("user store: apply schema: %w", err)
	}
	s := &Store{db: db, log: log}

	// Seed if empty
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err == nil && count == 0 {
		if err := s.seed(); err != nil {
			log.Error("user store: seed failed", slog.Any("err", err))
		}
	}
	return s, nil
}

// Close is a no-op: the caller owns the shared *sql.DB and is responsible for closing it.
func (s *Store) Close() error { return nil }

// ── seed ─────────────────────────────────────────────────────────────────────

type seedUser struct {
	Username    string
	DisplayName string
	Email       string
	Role        userv1.Role
	Password    string
}

func (s *Store) seed() error {
	seeds := []seedUser{
		{
			Username:    envStr("SEED_TONY_USER", "tony-stark"),
			DisplayName: "Tony Stark",
			Email:       envStr("SEED_TONY_EMAIL", "tony@stark.industries"),
			Role:        userv1.Role_ROLE_ADMIN,
			Password:    envStr("SEED_TONY_PASSWORD", "tony-stark"),
		},
	}
	for _, u := range seeds {
		if _, err := s.createInternal(context.Background(), u); err != nil {
			return fmt.Errorf("seed %s: %w", u.Username, err)
		}
		s.log.Info("user store: seeded user", slog.String("username", u.Username), slog.String("role", u.Role.String()))
	}
	return nil
}

// ── public methods ────────────────────────────────────────────────────────────

// Create hashes the plain-text password and inserts a new user.
func (s *Store) Create(ctx context.Context, username, email, displayName, password string, role userv1.Role) (*userv1.User, error) {
	return s.createInternal(ctx, seedUser{
		Username: username, Email: email, DisplayName: displayName,
		Role: role, Password: password,
	})
}

func (s *Store) createInternal(ctx context.Context, u seedUser) (*userv1.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("user store: hash password: %w", err)
	}
	id := uuid.New().String()
	roleStr := roleToString(u.Role)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (id, username, email, display_name, role, password_hash) VALUES (?,?,?,?,?,?)`,
		id, u.Username, u.Email, u.DisplayName, roleStr, string(hash),
	)
	if err != nil {
		return nil, fmt.Errorf("user store: insert user: %w", err)
	}
	return s.GetByID(ctx, id)
}

// CheckPassword verifies the plain-text password against the stored bcrypt hash.
// Returns the user and role on success.
func (s *Store) CheckPassword(ctx context.Context, username string, password []byte) (*userv1.User, error) {
	var hash string
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, password_hash FROM users WHERE username = ? AND is_active = 1`, username,
	).Scan(&id, &hash)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("user store: lookup: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), password); err != nil {
		return nil, fmt.Errorf("invalid password")
	}
	// Update last_login_at — best-effort, ignore error
	_, _ = s.db.ExecContext(ctx,
		`UPDATE users SET updated_at = datetime('now') WHERE id = ?`, id)
	return s.GetByID(ctx, id)
}

// GetByID returns a user by UUID.
func (s *Store) GetByID(ctx context.Context, id string) (*userv1.User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,username,email,display_name,role,is_active,created_at,updated_at FROM users WHERE id = ?`, id)
	return scanUser(row)
}

// GetByUsername returns a user by username.
func (s *Store) GetByUsername(ctx context.Context, username string) (*userv1.User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,username,email,display_name,role,is_active,created_at,updated_at FROM users WHERE username = ?`, username)
	return scanUser(row)
}

// List returns all users, optionally filtered by role.
func (s *Store) List(ctx context.Context, role userv1.Role, activeOnly bool) ([]*userv1.User, error) {
	q := `SELECT id,username,email,display_name,role,is_active,created_at,updated_at FROM users WHERE 1=1`
	var args []any
	if role != userv1.Role_ROLE_UNSPECIFIED {
		q += ` AND role = ?`
		args = append(args, roleToString(role))
	}
	if activeOnly {
		q += ` AND is_active = 1`
	}
	q += ` ORDER BY username`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("user store: list: %w", err)
	}
	defer rows.Close()
	var out []*userv1.User
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UpdateProfile updates display_name and/or email for a user.
func (s *Store) UpdateProfile(ctx context.Context, id, email, displayName string) (*userv1.User, error) {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET email=?, display_name=?, updated_at=datetime('now') WHERE id=?`,
		email, displayName, id)
	if err != nil {
		return nil, fmt.Errorf("user store: update profile: %w", err)
	}
	return s.GetByID(ctx, id)
}

// UpdateUser updates role and is_active for a user (admin operation).
func (s *Store) UpdateUser(ctx context.Context, id string, role userv1.Role, isActive bool) (*userv1.User, error) {
	_, err := s.db.ExecContext(ctx,
		`UPDATE users SET role=?, is_active=?, updated_at=datetime('now') WHERE id=?`,
		roleToString(role), boolToInt(isActive), id)
	if err != nil {
		return nil, fmt.Errorf("user store: update user: %w", err)
	}
	return s.GetByID(ctx, id)
}

// ChangePassword verifies current_password then replaces the hash.
func (s *Store) ChangePassword(ctx context.Context, id string, currentPassword, newPassword []byte) error {
	var hash string
	err := s.db.QueryRowContext(ctx,
		`SELECT password_hash FROM users WHERE id = ?`, id).Scan(&hash)
	if err != nil {
		return fmt.Errorf("user store: user not found")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), currentPassword); err != nil {
		return fmt.Errorf("current password is incorrect")
	}
	newHash, err := bcrypt.GenerateFromPassword(newPassword, bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("user store: hash new password: %w", err)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE users SET password_hash=?, updated_at=datetime('now') WHERE id=?`,
		string(newHash), id)
	return err
}

// Delete removes a user by ID.
func (s *Store) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("user store: delete: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("user not found")
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func scanUser(row *sql.Row) (*userv1.User, error) {
	var u userv1.User
	var roleStr string
	var isActive int
	var createdAt, updatedAt string
	err := row.Scan(&u.Id, &u.Username, &u.Email, &u.DisplayName,
		&roleStr, &isActive, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("user store: scan: %w", err)
	}
	u.Role = roleFromString(roleStr)
	u.IsActive = isActive == 1
	u.CreatedAt = parseTS(createdAt)
	u.UpdatedAt = parseTS(updatedAt)
	return &u, nil
}

func scanUserRow(rows *sql.Rows) (*userv1.User, error) {
	var u userv1.User
	var roleStr string
	var isActive int
	var createdAt, updatedAt string
	err := rows.Scan(&u.Id, &u.Username, &u.Email, &u.DisplayName,
		&roleStr, &isActive, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("user store: scan row: %w", err)
	}
	u.Role = roleFromString(roleStr)
	u.IsActive = isActive == 1
	u.CreatedAt = parseTS(createdAt)
	u.UpdatedAt = parseTS(updatedAt)
	return &u, nil
}

func roleToString(r userv1.Role) string {
	switch r {
	case userv1.Role_ROLE_ADMIN:
		return "ROLE_ADMIN"
	case userv1.Role_ROLE_EDITOR:
		return "ROLE_EDITOR"
	default:
		return "ROLE_VIEWER"
	}
}

func roleFromString(s string) userv1.Role {
	switch s {
	case "ROLE_ADMIN":
		return userv1.Role_ROLE_ADMIN
	case "ROLE_EDITOR":
		return userv1.Role_ROLE_EDITOR
	default:
		return userv1.Role_ROLE_VIEWER
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func parseTS(s string) *timestamppb.Timestamp {
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05Z"} {
		if t, err := time.Parse(layout, s); err == nil {
			return timestamppb.New(t)
		}
	}
	return timestamppb.Now()
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
