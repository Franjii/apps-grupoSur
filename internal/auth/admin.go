package auth

import "strings"

// Module es una entrada del catálogo de módulos.
type Module struct {
	Key  string
	Name string
}

// ListModules devuelve el catálogo de módulos.
func (s *Store) ListModules() ([]Module, error) {
	rows, err := s.db.Query(`SELECT key, name FROM modules ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Module
	for rows.Next() {
		var m Module
		if err := rows.Scan(&m.Key, &m.Name); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListUsers devuelve todos los usuarios con sus módulos.
func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY email`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, u := range users {
		if u.Modules, err = s.modulesFor(u.ID); err != nil {
			return nil, err
		}
	}
	return users, nil
}

// CreateUser crea (o reactiva) un usuario y asigna sus módulos.
func (s *Store) CreateUser(email, name string, isAdmin bool, modules []string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	res, err := s.db.Exec(
		`INSERT INTO users(email, name, is_admin, active) VALUES (?,?,?,1)
		 ON CONFLICT(email) DO UPDATE SET name=excluded.name, is_admin=excluded.is_admin, active=1`,
		email, name, isAdmin)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		s.db.QueryRow(`SELECT id FROM users WHERE email=?`, email).Scan(&id)
	}
	if isAdmin {
		// admin: todos los módulos
		_, err = s.db.Exec(
			`INSERT INTO user_modules(user_id, module_key)
			 SELECT ?, key FROM modules ON CONFLICT DO NOTHING`, id)
		return err
	}
	return s.SetUserModules(id, modules)
}

// SetUserModules reemplaza los módulos asignados a un usuario.
func (s *Store) SetUserModules(userID int64, modules []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM user_modules WHERE user_id=?`, userID); err != nil {
		return err
	}
	for _, m := range modules {
		if _, err := tx.Exec(
			`INSERT INTO user_modules(user_id, module_key) VALUES (?,?)
			 ON CONFLICT DO NOTHING`, userID, m); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetUserActive habilita o inhabilita un usuario (y borra sus sesiones si se inhabilita).
func (s *Store) SetUserActive(userID int64, active bool) error {
	if _, err := s.db.Exec(`UPDATE users SET active=? WHERE id=?`, active, userID); err != nil {
		return err
	}
	if !active {
		s.db.Exec(`DELETE FROM sessions WHERE user_id=?`, userID)
	}
	return nil
}
