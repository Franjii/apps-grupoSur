package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const tokenExpiry = 24 * time.Hour

type PasswordResetService struct {
	db *sql.DB
}

func NewPasswordResetService(db *sql.DB) *PasswordResetService {
	return &PasswordResetService{db: db}
}

// GenerateToken creates a password reset token for the given user.
// Invalidates any existing unused tokens for the same user.
func (s *PasswordResetService) GenerateToken(ctx context.Context, userID string) (string, error) {
	// Invalidate existing unused tokens for this user
	_, err := s.db.ExecContext(ctx,
		"UPDATE password_reset_tokens SET used = 1 WHERE user_id = ? AND used = 0",
		userID,
	)
	if err != nil {
		return "", fmt.Errorf("invalidating existing tokens: %w", err)
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random token: %w", err)
	}
	rawToken := hex.EncodeToString(b)

	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	expiresAt := time.Now().Add(tokenExpiry)

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO password_reset_tokens (user_id, token_hash, expires_at) VALUES (?, ?, ?)`,
		userID, tokenHash, expiresAt,
	)
	if err != nil {
		return "", fmt.Errorf("storing reset token: %w", err)
	}

	return rawToken, nil
}

// fetchAndValidateToken hashes the raw token, looks it up via the provided querier,
// and validates that it has not been used or expired.
// Returns the associated user ID and token hash on success.
func fetchAndValidateToken(ctx context.Context, qr querier, rawToken string) (string, string, error) {
	hash := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(hash[:])

	var userID string
	var expiresAt time.Time
	var used bool

	err := qr.QueryRowContext(ctx,
		`SELECT user_id, expires_at, used FROM password_reset_tokens WHERE token_hash = ?`,
		tokenHash,
	).Scan(&userID, &expiresAt, &used)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrTokenInvalid
		}
		return "", "", fmt.Errorf("looking up token: %w", err)
	}

	if used {
		return "", "", ErrTokenUsed
	}
	if time.Now().After(expiresAt) {
		return "", "", ErrTokenExpired
	}

	return userID, tokenHash, nil
}

// ValidateToken checks if a raw token is valid (exists, not used, not expired).
// Returns the associated user ID on success.
func (s *PasswordResetService) ValidateToken(ctx context.Context, rawToken string) (string, error) {
	userID, _, err := fetchAndValidateToken(ctx, s.db, rawToken)
	return userID, err
}

// CleanupExpiredTokens deletes password reset tokens older than 7 days.
func (s *PasswordResetService) CleanupExpiredTokens(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM password_reset_tokens WHERE created_at < datetime('now', '-7 days')",
	)
	if err != nil {
		return fmt.Errorf("cleaning up expired tokens: %w", err)
	}
	return nil
}

// ResetPassword validates the token, updates the user's password, and marks the token as used.
// Returns the user ID associated with the token on success.
func (s *PasswordResetService) ResetPassword(ctx context.Context, rawToken, newPassword string) (string, error) {
	if len(newPassword) < MinPasswordLength {
		return "", ErrPasswordTooShort
	}
	if len(newPassword) > MaxPasswordLength {
		return "", ErrPasswordTooLong
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	userID, tokenHash, err := fetchAndValidateToken(ctx, tx, rawToken)
	if err != nil {
		return "", err
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		"UPDATE users SET password_hash = ? WHERE id = ?",
		string(passwordHash), userID,
	)
	if err != nil {
		return "", fmt.Errorf("updating password: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		"UPDATE password_reset_tokens SET used = 1 WHERE token_hash = ?",
		tokenHash,
	)
	if err != nil {
		return "", fmt.Errorf("marking token as used: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return userID, nil
}
