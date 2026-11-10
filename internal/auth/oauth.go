package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// SessionTTL es la vigencia de la sesión.
const SessionTTL = 7 * 24 * time.Hour

// ConfigureOAuth habilita el login con Google en el Manager.
func (m *Manager) ConfigureOAuth(clientID, clientSecret, redirectURL, afterLoginURL string) {
	m.oauth = &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
	m.afterLogin = afterLoginURL
}

// LoginHandler inicia el flujo de login (o usa el bypass de desarrollo).
func (m *Manager) LoginHandler(w http.ResponseWriter, r *http.Request) {
	// Bypass de desarrollo: crea sesión directa.
	if m.DevEmail != "" {
		u, err := m.store.UserByEmail(m.DevEmail)
		if err != nil || !u.Active {
			http.Error(w, "usuario de desarrollo no habilitado", http.StatusForbidden)
			return
		}
		m.startSession(w, u)
		http.Redirect(w, r, m.afterLoginOr("/"), http.StatusSeeOther)
		return
	}
	if m.oauth == nil {
		http.Error(w, "OAuth no configurado", http.StatusInternalServerError)
		return
	}
	state := randomState()
	http.SetCookie(w, &http.Cookie{
		Name: "gs_oauth_state", Value: state, Path: "/",
		MaxAge: 600, HttpOnly: true, Secure: m.secure, SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, m.oauth.AuthCodeURL(state, oauth2.AccessTypeOnline), http.StatusSeeOther)
}

// CallbackHandler procesa el retorno de Google.
func (m *Manager) CallbackHandler(loginPage func(w http.ResponseWriter, r *http.Request, errMsg string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st, err := r.Cookie("gs_oauth_state")
		if err != nil || st.Value == "" || st.Value != r.URL.Query().Get("state") {
			loginPage(w, r, "Sesión de login inválida, reintentá.")
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			loginPage(w, r, "No se recibió el código de autorización.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		tok, err := m.oauth.Exchange(ctx, code)
		if err != nil {
			loginPage(w, r, "No se pudo completar el login con Google.")
			return
		}
		email, name, err := fetchGoogleProfile(ctx, m.oauth, tok)
		if err != nil {
			loginPage(w, r, "No se pudo obtener el perfil de Google.")
			return
		}
		u, err := m.store.UserByEmail(email)
		if err != nil || !u.Active {
			loginPage(w, r, "El usuario "+email+" no está habilitado. Pedile acceso a un administrador.")
			return
		}
		// Guardar nombre si está vacío.
		if u.Name == "" && name != "" {
			m.store.db.Exec(`UPDATE users SET name=? WHERE id=?`, name, u.ID)
		}
		m.startSession(w, u)
		http.Redirect(w, r, m.afterLoginOr("/"), http.StatusSeeOther)
	}
}

func (m *Manager) afterLoginOr(def string) string {
	if m.afterLogin != "" {
		return m.afterLogin
	}
	return def
}

// LogoutHandler cierra la sesión.
func (m *Manager) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		m.store.DeleteSession(c.Value)
	}
	m.ClearSessionCookie(w)
	http.Redirect(w, r, m.loginURL, http.StatusSeeOther)
}

func (m *Manager) startSession(w http.ResponseWriter, u *User) {
	tok, err := m.store.CreateSession(u.ID, SessionTTL)
	if err != nil {
		http.Error(w, "no se pudo crear la sesión", http.StatusInternalServerError)
		return
	}
	m.SetSessionCookie(w, tok, SessionTTL)
}

func fetchGoogleProfile(ctx context.Context, cfg *oauth2.Config, tok *oauth2.Token) (email, name string, err error) {
	client := cfg.Client(ctx, tok)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", errors.New("userinfo status " + resp.Status)
	}
	var p struct {
		Email         string `json:"email"`
		VerifiedEmail bool   `json:"verified_email"`
		Name          string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return "", "", err
	}
	if p.Email == "" {
		return "", "", errors.New("email vacío")
	}
	return strings.ToLower(p.Email), p.Name, nil
}

func randomState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
