package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
)

// pizarraUsers exposes only active accounts eligible for project membership.
// Both the server secret and an active Pizarra session are required.
func (s *Server) pizarraUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.SSOSecret == "" {
		http.NotFound(w, r)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-SSO-Secret")), []byte(s.SSOSecret)) != 1 {
		http.Error(w, "unauthorized", 401)
		return
	}
	u, err := s.Store.UserBySession(r.Header.Get("X-Session-Token"))
	if err != nil || u == nil {
		http.Error(w, "invalid session", 401)
		return
	}
	if !u.HasModule("pizarra") {
		http.Error(w, "forbidden", 403)
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `SELECT u.email,u.name FROM users u WHERE u.active=1 AND (u.is_admin=1 OR EXISTS(SELECT 1 FROM user_modules m WHERE m.user_id=u.id AND m.module_key='pizarra')) ORDER BY u.email`)
	if err != nil {
		http.Error(w, "directory unavailable", 503)
		return
	}
	defer rows.Close()
	type member struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	users := []member{}
	for rows.Next() {
		var user member
		if err = rows.Scan(&user.Email, &user.Name); err != nil {
			http.Error(w, "directory unavailable", 503)
			return
		}
		users = append(users, user)
	}
	if rows.Err() != nil {
		http.Error(w, "directory unavailable", 503)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(users)
}
