// Package auth maneja usuarios, sesiones y control de acceso por módulo.
package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

// User es el usuario autenticado y sus módulos habilitados.
type User struct {
	ID      int64
	Email   string
	Name    string
	IsAdmin bool
	Active  bool
	Modules []string
}

func (u *User) HasModule(key string) bool {
	if u.IsAdmin {
		return true
	}
	for _, m := range u.Modules {
		if m == key {
			return true
		}
	}
	return false
}

// Store accede a usuarios y sesiones.
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

var ErrNotFound = errors.New("usuario no encontrado")

// rowScanner abstrae *sql.Row y *sql.Rows para compartir el escaneo de usuarios.
type rowScanner interface{ Scan(dest ...any) error }

// userCols son las columnas que scanUser espera, en orden.
const userCols = "id, email, name, is_admin, active"

// scanUser escanea las columnas userCols en un User. El driver mapea las
// columnas INTEGER 0/1 (is_admin, active) directo a bool.
func scanUser(sc rowScanner) (*User, error) {
	u := &User{}
	err := sc.Scan(&u.ID, &u.Email, &u.Name, &u.IsAdmin, &u.Active)
	return u, err
}

// UserByEmail carga un usuario activo y sus módulos.
func (s *Store) UserByEmail(email string) (*User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	u, err := scanUser(s.db.QueryRow(
		`SELECT `+userCols+` FROM users WHERE email = ?`, email))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Modules, err = s.modulesFor(u.ID)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func (s *Store) userByID(id int64) (*User, error) {
	u, err := scanUser(s.db.QueryRow(
		`SELECT `+userCols+` FROM users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.Modules, err = s.modulesFor(u.ID)
	return u, err
}

func (s *Store) modulesFor(userID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT module_key FROM user_modules WHERE user_id = ? ORDER BY module_key`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var mods []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		mods = append(mods, m)
	}
	return mods, rows.Err()
}

// CreateSession crea una sesión para el usuario y devuelve su token.
func (s *Store) CreateSession(userID int64, ttl time.Duration) (string, error) {
	tok := randomToken()
	expires := time.Now().Add(ttl).UTC().Format("2006-01-02T15:04:05Z")
	_, err := s.db.Exec(
		`INSERT INTO sessions(id, user_id, expires_at) VALUES (?,?,?)`, tok, userID, expires)
	if err != nil {
		return "", err
	}
	return tok, nil
}

// UserBySession devuelve el usuario de una sesión válida (no expirada).
func (s *Store) UserBySession(token string) (*User, error) {
	var userID int64
	var expires string
	err := s.db.QueryRow(
		`SELECT user_id, expires_at FROM sessions WHERE id = ?`, token,
	).Scan(&userID, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z", expires); err == nil && time.Now().After(t) {
		s.DeleteSession(token)
		return nil, ErrNotFound
	}
	u, err := s.userByID(userID)
	if err != nil {
		return nil, err
	}
	if !u.Active {
		return nil, ErrNotFound
	}
	return u, nil
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, token)
	return err
}

func randomToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ── Contexto ───────────────────────────────────────────────────────────────

type ctxKey int

const userKey ctxKey = 0

func withUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

// FromContext devuelve el usuario autenticado de la request (o nil).
func FromContext(ctx context.Context) *User {
	u, _ := ctx.Value(userKey).(*User)
	return u
}

// helper para handlers
func WithUser(r *http.Request, u *User) *http.Request {
	return r.WithContext(withUser(r.Context(), u))
}
