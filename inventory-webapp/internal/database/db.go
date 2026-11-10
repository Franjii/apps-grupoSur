package database

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	_ "modernc.org/sqlite"
)

// Open initializes the SQLite database, runs migrations and seeds initial data.
func Open(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling WAL mode: %w", err)
	}

	// Enable foreign key constraints
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("enabling foreign keys: %w", err)
	}

	// Set busy timeout for concurrent access
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting busy timeout: %w", err)
	}

	// WAL mode allows concurrent reads. Allow a small pool so background
	// goroutines don't block HTTP request queries.
	db.SetMaxOpenConns(4)

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	if err := seed(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("seeding data: %w", err)
	}

	slog.Info("Database initialized", "path", dbPath)
	return db, nil
}

func migrate(db *sql.DB) error {
	// These CREATE TABLE IF NOT EXISTS statements match the desktop database schema.
	// The desktop DB already has these tables with data; this ensures a fresh DB works too.
	tables := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			name TEXT NOT NULL,
			role TEXT NOT NULL CHECK (role IN ('support', 'requester', 'admin', 'user')),
			requester_type TEXT CHECK (requester_type IN ('laboratory', 'medical_team', 'regulatory', 'pharmacy', 'administration')),
			active BOOLEAN DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS studies (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			platform_1 TEXT,
			platform_2 TEXT,
			platform_3 TEXT,
			active BOOLEAN DEFAULT 1,
			created_by TEXT REFERENCES users(id),
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS supply_definitions (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL CHECK (type IN ('single_use', 'returnable', 'disposable')),
			study_id TEXT NOT NULL REFERENCES studies(id) ON DELETE CASCADE,
			has_expiration_date BOOLEAN DEFAULT 0,
			has_serial_number BOOLEAN DEFAULT 0,
			expiration_alert_enabled BOOLEAN DEFAULT 0,
			active BOOLEAN DEFAULT 1,
			created_by TEXT REFERENCES users(id),
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS supply_instances (
			id TEXT PRIMARY KEY,
			supply_definition_id TEXT NOT NULL REFERENCES supply_definitions(id) ON DELETE CASCADE,
			study_id TEXT NOT NULL REFERENCES studies(id) ON DELETE CASCADE,
			quantity INTEGER DEFAULT 0 CHECK (quantity >= 0),
			expiration_date DATE,
			lot_number TEXT,
			reorder_alert_quantity INTEGER,
			expiration_alert_days INTEGER,
			received_date DATETIME DEFAULT CURRENT_TIMESTAMP,
			received_by TEXT REFERENCES users(id),
			notes TEXT,
			active BOOLEAN DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS supply_instance_serial_numbers (
			id TEXT PRIMARY KEY,
			supply_instance_id TEXT NOT NULL REFERENCES supply_instances(id) ON DELETE CASCADE,
			serial_number TEXT NOT NULL UNIQUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS stock_operations (
			id TEXT PRIMARY KEY,
			study_id TEXT REFERENCES studies(id) ON DELETE CASCADE,
			supply_instance_id TEXT REFERENCES supply_instances(id) ON DELETE CASCADE,
			operation TEXT NOT NULL CHECK (operation IN ('delivery', 'return', 'use')),
			quantity INTEGER NOT NULL CHECK (quantity > 0),
			notes TEXT,
			performed_by TEXT REFERENCES users(id),
			operation_date DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS stock_operation_serial_numbers (
			id TEXT PRIMARY KEY,
			stock_operation_id TEXT NOT NULL REFERENCES stock_operations(id) ON DELETE CASCADE,
			serial_number TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS current_stock (
			id TEXT PRIMARY KEY,
			supply_instance_id TEXT REFERENCES supply_instances(id) ON DELETE CASCADE,
			study_id TEXT REFERENCES studies(id) ON DELETE CASCADE,
			quantity INTEGER DEFAULT 0 CHECK (quantity >= 0),
			last_updated DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(supply_instance_id, study_id)
		)`,

		`CREATE TABLE IF NOT EXISTS returnable_items (
			id TEXT PRIMARY KEY,
			supply_instance_id TEXT REFERENCES supply_instances(id) ON DELETE CASCADE,
			study_id TEXT REFERENCES studies(id) ON DELETE CASCADE,
			serial_number TEXT UNIQUE NOT NULL,
			status TEXT DEFAULT 'not_delivered' CHECK (status IN ('not_delivered', 'delivered', 'returned', 'frozen')),
			current_participant_id TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS delivery_log (
			id TEXT PRIMARY KEY,
			participant_id TEXT NOT NULL,
			item_id TEXT REFERENCES returnable_items(id) ON DELETE SET NULL,
			supply_name TEXT NOT NULL,
			serial_number TEXT NOT NULL,
			operation TEXT NOT NULL CHECK (operation IN ('delivery', 'return', 'use', 'freeze', 'unfreeze')),
			operation_date DATETIME DEFAULT CURRENT_TIMESTAMP,
			performed_by TEXT REFERENCES users(id)
		)`,

		`CREATE TABLE IF NOT EXISTS alerts (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			supply_id TEXT REFERENCES supply_instances(id) ON DELETE CASCADE,
			study_id TEXT REFERENCES studies(id),
			message TEXT NOT NULL,
			read BOOLEAN DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS password_reset_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT NOT NULL REFERENCES users(id),
			token_hash TEXT UNIQUE NOT NULL,
			expires_at DATETIME NOT NULL,
			used BOOLEAN NOT NULL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS expiration_notifications_sent (
			id TEXT PRIMARY KEY,
			supply_instance_id TEXT NOT NULL REFERENCES supply_instances(id) ON DELETE CASCADE,
			milestone TEXT NOT NULL CHECK (milestone IN ('2_months', '1_month', '1_week', 'expired')),
			sent_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(supply_instance_id, milestone)
		)`,
	}

	for _, stmt := range tables {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("executing migration: %w\nSQL: %s", err, stmt)
		}
	}

	// Add username column to users table (needed for our auth, not in desktop DB)
	if err := addColumnIfNotExists(db, "users", "username", "TEXT"); err != nil {
		return err
	}

	// Create indexes on foreign key columns for query performance
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_supply_definitions_study_id ON supply_definitions(study_id)`,
		`CREATE INDEX IF NOT EXISTS idx_supply_definitions_active ON supply_definitions(active)`,
		`CREATE INDEX IF NOT EXISTS idx_supply_definitions_name ON supply_definitions(name)`,
		`CREATE INDEX IF NOT EXISTS idx_supply_instances_definition_id ON supply_instances(supply_definition_id)`,
		`CREATE INDEX IF NOT EXISTS idx_supply_instances_study_id ON supply_instances(study_id)`,
		`CREATE INDEX IF NOT EXISTS idx_supply_instances_active ON supply_instances(active)`,
		`CREATE INDEX IF NOT EXISTS idx_instance_serial_numbers_instance_id ON supply_instance_serial_numbers(supply_instance_id)`,
		`CREATE INDEX IF NOT EXISTS idx_stock_operations_instance_id ON stock_operations(supply_instance_id)`,
		`CREATE INDEX IF NOT EXISTS idx_current_stock_instance_id ON current_stock(supply_instance_id)`,
		`CREATE INDEX IF NOT EXISTS idx_returnable_items_instance_id ON returnable_items(supply_instance_id)`,
		`CREATE INDEX IF NOT EXISTS idx_delivery_log_participant_id ON delivery_log(participant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_delivery_log_item_id ON delivery_log(item_id)`,
		`CREATE INDEX IF NOT EXISTS idx_delivery_log_operation_date ON delivery_log(operation_date)`,
		`CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_user_id ON password_reset_tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_expiration_notif_instance ON expiration_notifications_sent(supply_instance_id)`,
	}

	for _, stmt := range indexes {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("creating index: %w\nSQL: %s", err, stmt)
		}
	}

	// Migrate delivery_log CHECK constraint to include freeze/unfreeze operations
	if err := migrateDeliveryLogOperations(db); err != nil {
		return err
	}

	// Migrate delivery_log item_id from ON DELETE CASCADE to ON DELETE SET NULL
	if err := migrateDeliveryLogSetNull(db); err != nil {
		return err
	}

	slog.Info("Database migrations completed")
	return nil
}

// migrateDeliveryLogOperations expands the delivery_log CHECK constraint to include 'freeze' and 'unfreeze'.
// Uses rename→create→copy→drop pattern since SQLite doesn't support ALTER CHECK.
func migrateDeliveryLogOperations(db *sql.DB) error {
	var tableSql string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='delivery_log'`).Scan(&tableSql)
	if err != nil {
		// Table doesn't exist yet (handled by CREATE TABLE IF NOT EXISTS above)
		return nil
	}

	// Already migrated if 'freeze' is in the CHECK constraint
	if strings.Contains(tableSql, "'freeze'") {
		return nil
	}

	slog.Info("Migrating delivery_log CHECK constraint to include freeze/unfreeze operations")

	stmts := []string{
		`ALTER TABLE delivery_log RENAME TO delivery_log_old`,
		`CREATE TABLE delivery_log (
			id TEXT PRIMARY KEY,
			participant_id TEXT NOT NULL,
			item_id TEXT REFERENCES returnable_items(id) ON DELETE SET NULL,
			supply_name TEXT NOT NULL,
			serial_number TEXT NOT NULL,
			operation TEXT NOT NULL CHECK (operation IN ('delivery', 'return', 'use', 'freeze', 'unfreeze')),
			operation_date DATETIME DEFAULT CURRENT_TIMESTAMP,
			performed_by TEXT REFERENCES users(id)
		)`,
		`INSERT INTO delivery_log SELECT * FROM delivery_log_old`,
		`DROP TABLE delivery_log_old`,
		`CREATE INDEX IF NOT EXISTS idx_delivery_log_participant_id ON delivery_log(participant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_delivery_log_item_id ON delivery_log(item_id)`,
		`CREATE INDEX IF NOT EXISTS idx_delivery_log_operation_date ON delivery_log(operation_date)`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migrating delivery_log operations: %w\nSQL: %s", err, stmt)
		}
	}

	slog.Info("delivery_log CHECK constraint migration completed")
	return nil
}

// migrateDeliveryLogSetNull changes delivery_log.item_id from ON DELETE CASCADE to ON DELETE SET NULL
// so that delivery history is preserved when returnable_items are removed.
func migrateDeliveryLogSetNull(db *sql.DB) error {
	var tableSql string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='delivery_log'`).Scan(&tableSql)
	if err != nil {
		return nil
	}

	// Already migrated if SET NULL is present
	if strings.Contains(tableSql, "SET NULL") {
		return nil
	}

	// Only migrate if CASCADE is still in use
	if !strings.Contains(tableSql, "ON DELETE CASCADE") {
		return nil
	}

	slog.Info("Migrating delivery_log item_id from ON DELETE CASCADE to ON DELETE SET NULL")

	stmts := []string{
		`ALTER TABLE delivery_log RENAME TO delivery_log_old`,
		`CREATE TABLE delivery_log (
			id TEXT PRIMARY KEY,
			participant_id TEXT NOT NULL,
			item_id TEXT REFERENCES returnable_items(id) ON DELETE SET NULL,
			supply_name TEXT NOT NULL,
			serial_number TEXT NOT NULL,
			operation TEXT NOT NULL CHECK (operation IN ('delivery', 'return', 'use', 'freeze', 'unfreeze')),
			operation_date DATETIME DEFAULT CURRENT_TIMESTAMP,
			performed_by TEXT REFERENCES users(id)
		)`,
		`INSERT INTO delivery_log SELECT * FROM delivery_log_old`,
		`DROP TABLE delivery_log_old`,
		`CREATE INDEX IF NOT EXISTS idx_delivery_log_participant_id ON delivery_log(participant_id)`,
		`CREATE INDEX IF NOT EXISTS idx_delivery_log_item_id ON delivery_log(item_id)`,
		`CREATE INDEX IF NOT EXISTS idx_delivery_log_operation_date ON delivery_log(operation_date)`,
	}

	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migrating delivery_log SET NULL: %w\nSQL: %s", err, stmt)
		}
	}

	slog.Info("delivery_log SET NULL migration completed")
	return nil
}

// addColumnIfNotExists adds a column to a table if it doesn't already exist.
// SAFETY: table, column, and colDef must be compile-time constants — never user input.
func addColumnIfNotExists(db *sql.DB, table, column, colDef string) error {
	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, colDef)
	_, err := db.Exec(stmt)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate column name") {
			return nil
		}
		return fmt.Errorf("adding column %s.%s: %w", table, column, err)
	}
	return nil
}
