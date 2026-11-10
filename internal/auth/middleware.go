package auth

import (
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

const cookieName = "gs_session"

// Manager agrupa la configuración de sesión y expone middlewares.
type Manager struct {
	store        *Store
	cookieDomain string
	secure       bool
	loginURL     string

	// Desarrollo: si DevEmail != "", se saltea OAuth y se usa ese usuario.
	DevEmail string

	// OAuth (configurado vía ConfigureOAuth).
	oauth      *oauth2.Config
	afterLogin string
}

func NewManager(store *Store, cookieDomain string, secure bool, loginURL, devEmail string) *Manager {
	return &Manager{
		store:        store,
		cookieDomain: cookieDomain,
		secure:       secure,
		loginURL:     loginURL,
		DevEmail:     devEmail,
	}
}

func (m *Manager) Store() *Store { return m.store }

// SetSessionCookie escribe la cookie de sesión (dominio padre para SSO).
func (m *Manager) SetSessionCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		Domain:   m.cookieDomain,
		Expires:  time.Now().Add(ttl),
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (m *Manager) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		Domain:   m.cookieDomain,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// currentUser resuelve el usuario de la request (cookie o bypass de desarrollo).
func (m *Manager) currentUser(r *http.Request) *User {
	if m.DevEmail != "" {
		if u, err := m.store.UserByEmail(m.DevEmail); err == nil && u.Active {
			return u
		}
	}
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := m.store.UserBySession(c.Value)
	if err != nil {
		return nil
	}
	return u
}

// RequireAuth exige sesión válida; si no, redirige al login.
func (m *Manager) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := m.currentUser(r)
		if u == nil {
			http.Redirect(w, r, m.loginURL, http.StatusSeeOther)
			return
		}
		next(w, WithUser(r, u))
	}
}

// RequireModule exige sesión y acceso al módulo indicado.
func (m *Manager) RequireModule(module string, next http.HandlerFunc) http.HandlerFunc {
	return m.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		u := FromContext(r.Context())
		if !u.HasModule(module) {
			http.Error(w, "No tenés acceso a este módulo.", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// RequireAdmin exige sesión de administrador.
func (m *Manager) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return m.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		u := FromContext(r.Context())
		if !u.IsAdmin {
			http.Error(w, "Requiere administrador.", http.StatusForbidden)
			return
		}
		next(w, r)
	})
}

// OptionalUser adjunta el usuario si existe, sin exigir sesión.
func (m *Manager) OptionalUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if u := m.currentUser(r); u != nil {
			r = WithUser(r, u)
		}
		next(w, r)
	}
}
