package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// seed inserts initial data if the database is empty.
func seed(db *sql.DB) error {
	if err := seedAdmin(db); err != nil {
		return fmt.Errorf("seeding admin user: %w", err)
	}
	return nil
}

func seedAdmin(db *sql.DB) error {
	ctx := context.Background()

	// Check if admin user exists by username
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE username = ?", "amarziotti").Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	// Also check by email (desktop DB may have the user without username)
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE email = ?", "agustin.marziotti2@gruposur.ar").Scan(&count)
	if err != nil {
		return err
	}

	if count > 0 {
		// User exists by email but no username — add username
		_, err = db.ExecContext(ctx,
			"UPDATE users SET username = ?, role = 'support' WHERE email = ?",
			"amarziotti", "agustin.marziotti2@gruposur.ar",
		)
		if err != nil {
			return fmt.Errorf("updating admin username: %w", err)
		}
		slog.Info("Updated existing admin user with username")
		return nil
	}

	// Create new admin user with UUID
	hash, err := bcrypt.GenerateFromPassword([]byte("admin123"), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	id := uuid.New().String()
	_, err = db.ExecContext(ctx,
		`INSERT INTO users (id, username, email, password_hash, name, role, active)
		 VALUES (?, ?, ?, ?, ?, ?, 1)`,
		id, "amarziotti", "agustin.marziotti2@gruposur.ar", string(hash), "Agustin Marziotti", "support",
	)
	if err != nil {
		return err
	}

	slog.Info("Seeded admin user")
	return nil
}
