// Package db abre la base SQLite, aplica el esquema y siembra datos iniciales.
package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Open abre (o crea) la base en dbPath, aplica pragmas y el esquema.
func Open(dbPath string) (*sql.DB, error) {
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("crear dir de base: %w", err)
		}
	}

	dsn := dbPath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	sdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir sqlite: %w", err)
	}
	// SQLite escribe de a un writer; un solo conn evita "database is locked".
	sdb.SetMaxOpenConns(1)

	if err := sdb.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := sdb.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("aplicar esquema: %w", err)
	}
	return sdb, nil
}

// Seed siembra el catálogo de módulos y los admins iniciales.
func Seed(sdb *sql.DB, adminEmails []string) error {
	if _, err := sdb.Exec(
		`INSERT INTO modules(key, name) VALUES
		     ('estadisticas','Estadísticas'),
		     ('inventario','Inventario'),
		     ('compliance','Compliance'), ('pizarra','Pizarra')
		 ON CONFLICT(key) DO NOTHING`); err != nil {
		return fmt.Errorf("seed modules: %w", err)
	}
	// Migración suave: el módulo antes se llamaba 'visitas' (ahora 'visitas' es
	// una sección del módulo 'estadisticas'). Reasignar y limpiar.
	if _, err := sdb.Exec(
		`UPDATE OR IGNORE user_modules SET module_key='estadisticas' WHERE module_key='visitas'`); err != nil {
		return fmt.Errorf("migrar user_modules: %w", err)
	}
	if _, err := sdb.Exec(`DELETE FROM modules WHERE key='visitas'`); err != nil {
		return fmt.Errorf("limpiar módulo viejo: %w", err)
	}

	for _, email := range adminEmails {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" {
			continue
		}
		if _, err := sdb.Exec(
			`INSERT INTO users(email, name, is_admin, active) VALUES (?, '', 1, 1)
			 ON CONFLICT(email) DO UPDATE SET is_admin=1, active=1`, email); err != nil {
			return fmt.Errorf("seed admin %s: %w", email, err)
		}
		// Los admins tienen acceso a todos los módulos existentes.
		if _, err := sdb.Exec(
			`INSERT INTO user_modules(user_id, module_key)
			 SELECT u.id, m.key FROM users u, modules m WHERE u.email = ?
			 ON CONFLICT DO NOTHING`, email); err != nil {
			return fmt.Errorf("seed admin modules %s: %w", email, err)
		}
	}
	return nil
}
