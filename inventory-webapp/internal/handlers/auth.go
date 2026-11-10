package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"inventory-webapp/internal/services"
	"inventory-webapp/templates/pages"
)

type AuthHandler struct {
	authService *services.AuthService
	sessions    *SessionStore
	sso         *SSOBridge
}

func NewAuthHandler(authService *services.AuthService, sessions *SessionStore, sso *SSOBridge) *AuthHandler {
	return &AuthHandler{
		authService: authService,
		sessions:    sessions,
		sso:         sso,
	}
}

// RegisterRoutes registers public auth routes on the mux.
func (h *AuthHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /login", h.ShowLogin)
	mux.HandleFunc("POST /login", h.HandleLogin)
	mux.HandleFunc("POST /logout", h.HandleLogout)
}

// knownLoginErrors is the set of error messages that may appear on the login page.
var knownLoginErrors = map[string]bool{
	"Error procesando formulario":          true,
	"Usuario y contraseña son requeridos":  true,
	"Credenciales inválidas":               true,
	"Cuenta desactivada":                   true,
	"Error interno":                        true,
}

// knownLoginSuccesses is the set of success messages that may appear on the login page.
var knownLoginSuccesses = map[string]bool{
	"Contraseña actualizada correctamente. Inicie sesión.": true,
}

// ShowLogin renders the login page. With SSO enabled, the local username/password
// form is disabled and users are sent to the platform login (single sign-on).
func (h *AuthHandler) ShowLogin(w http.ResponseWriter, r *http.Request) {
	if h.sso.Enabled() {
		http.Redirect(w, r, h.sso.LoginURL(), http.StatusSeeOther)
		return
	}
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if _, ok := h.sessions.Get(cookie.Value); ok {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
	}

	errorMsg := r.URL.Query().Get("error")
	if !knownLoginErrors[errorMsg] {
		errorMsg = ""
	}
	successMsg := r.URL.Query().Get("success")
	if !knownLoginSuccesses[successMsg] {
		successMsg = ""
	}
	render(w, r, pages.LoginPage(errorMsg, successMsg))
}

// HandleLogin processes the login form submission. Disabled under SSO.
func (h *AuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if h.sso.Enabled() {
		http.Redirect(w, r, h.sso.LoginURL(), http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/login?error=Error+procesando+formulario", http.StatusSeeOther)
		return
	}

	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")

	if username == "" || password == "" {
		http.Redirect(w, r, "/login?error=Usuario+y+contrase%C3%B1a+son+requeridos", http.StatusSeeOther)
		return
	}

	user, err := h.authService.Authenticate(r.Context(), username, password)
	if err != nil {
		if errors.Is(err, services.ErrInvalidCredentials) {
			http.Redirect(w, r, "/login?error=Credenciales+inv%C3%A1lidas", http.StatusSeeOther)
			return
		}
		if errors.Is(err, services.ErrUserInactive) {
			http.Redirect(w, r, "/login?error=Cuenta+desactivada", http.StatusSeeOther)
			return
		}
		slog.Error("authentication error", "error", err)
		http.Redirect(w, r, "/login?error=Error+interno", http.StatusSeeOther)
		return
	}

	sessionID, err := h.sessions.Create(user.ID)
	if err != nil {
		slog.Error("failed to create session", "error", err)
		http.Redirect(w, r, "/login?error=Error+interno", http.StatusSeeOther)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   sessionMaxAge,
		HttpOnly: true,
		Secure:   h.sessions.SecureCookie,
		SameSite: http.SameSiteLaxMode,
	})

	slog.Info("User logged in", "username", user.Username, "role", user.Role)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// HandleLogout destroys the session and redirects to login.
func (h *AuthHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	var userID string
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if id, ok := h.sessions.Get(cookie.Value); ok {
			userID = id
		}
		h.sessions.Delete(cookie.Value)
	}

	clearSessionCookie(w, h.sessions.SecureCookie)
	clearCSRFCookie(w, h.sessions.SecureCookie)
	slog.Info("User logged out", "user_id", userID)

	// Con SSO, cerrar sesión en la plataforma (fuente de verdad). El logout de la
	// plataforma limpia gs_session en .gruposur.ar y termina el SSO.
	if h.sso.Enabled() && h.sso.LogoutURL() != "" {
		http.Redirect(w, r, h.sso.LogoutURL(), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
