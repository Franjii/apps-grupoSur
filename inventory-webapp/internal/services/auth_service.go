package services

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"inventory-webapp/internal/models"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	db *sql.DB
}

func NewAuthService(db *sql.DB) *AuthService {
	return &AuthService{db: db}
}

// Authenticate verifies username and password against the database.
// Returns the User if credentials are valid and user is active.
func (s *AuthService) Authenticate(ctx context.Context, username, password string) (*models.User, error) {
	user, err := scanUser(s.db.QueryRowContext(ctx,
		"SELECT id, username, password_hash, name, email, role, active, created_at FROM users WHERE username = ?",
		username,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("authenticating user: %w", err)
	}

	if !user.Active {
		return nil, ErrUserInactive
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	return &user, nil
}

// GetUserByEmail returns an active user by email, or nil if not found.
func (s *AuthService) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	user, err := scanUserSafe(s.db.QueryRowContext(ctx,
		`SELECT id, username, name, email, role, active, created_at
		 FROM users WHERE email = ? AND active = 1`,
		email,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("looking up user by email: %w", err)
	}
	return &user, nil
}

// EnsureSSOUser resolves the local inventory user for an email authenticated by
// the platform (SSO bridge). If no active user exists it is auto-provisioned.
// The platform is the source of truth for identity; isAdmin maps platform admins
// to the inventory "admin" role, everyone else to "user".
func (s *AuthService) EnsureSSOUser(ctx context.Context, email, name string, isAdmin bool) (*models.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, errors.New("SSO: email requerido")
	}

	if user, err := s.GetUserByEmail(ctx, email); err != nil {
		return nil, err
	} else if user != nil {
		return user, nil
	}

	role := models.RoleUser
	if isAdmin {
		role = models.RoleAdmin
	}
	name = strings.TrimSpace(name)
	username := usernameFromEmail(email)
	if name == "" {
		name = username
	}
	// La contraseña local no se usa con SSO; se genera un hash inutilizable.
	hash, err := bcrypt.GenerateFromPassword([]byte(uuid.New().String()), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hashing SSO password: %w", err)
	}
	id := uuid.New().String()

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO users (id, username, email, password_hash, name, role, active)
		 VALUES (?, ?, ?, ?, ?, ?, 1)`,
		id, username, email, string(hash), name, role,
	)
	if err != nil {
		// Carrera: otra request lo pudo insertar (email es UNIQUE). Reintentar lectura.
		if user, gerr := s.GetUserByEmail(ctx, email); gerr == nil && user != nil {
			return user, nil
		}
		return nil, fmt.Errorf("provisioning SSO user: %w", err)
	}

	return &models.User{
		ID:       id,
		Username: username,
		FullName: name,
		Email:    &email,
		Role:     role,
		Active:   true,
	}, nil
}

// usernameFromEmail deriva un username legible a partir de la parte local del
// email. No es único (username no tiene restricción UNIQUE); sólo es cosmético.
func usernameFromEmail(email string) string {
	if i := strings.IndexByte(email, '@'); i > 0 {
		return email[:i]
	}
	return email
}

// GetActiveUserSafe retrieves an active user by ID without selecting password_hash.
// Used by session middleware where the password hash is not needed.
func (s *AuthService) GetActiveUserSafe(ctx context.Context, id string) (*models.User, error) {
	user, err := scanUserSafe(s.db.QueryRowContext(ctx,
		"SELECT id, username, name, email, role, active, created_at FROM users WHERE id = ? AND active = 1",
		id,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("getting user by ID: %w", err)
	}

	return &user, nil
}
