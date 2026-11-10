package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"inventory-webapp/internal/models"
	"inventory-webapp/internal/services"
)

// platformSessionCookie is the platform's SSO session cookie, set on the parent
// domain (.gruposur.ar) so it reaches this subdomain automatically.
const platformSessionCookie = "gs_session"

// ssoCacheTTL bounds how long an introspection result is cached per token, to
// avoid calling the platform on every request (including HTMX partials).
const ssoCacheTTL = 45 * time.Second

// SSOConfig configures the SSO bridge to the platform (login.gruposur.ar).
type SSOConfig struct {
	IntrospectURL  string // ej. https://login.gruposur.ar/auth/introspect
	SharedSecret   string // debe coincidir con SSO_SHARED_SECRET de la plataforma
	LoginURL       string // a dónde mandar a los no autenticados
	LogoutURL      string // a dónde mandar al cerrar sesión (logout de la plataforma)
	RequiredModule string // módulo RBAC exigido, ej. "inventario"
}

// platformIdentity is the introspection response from the platform.
type platformIdentity struct {
	Email   string   `json:"email"`
	Name    string   `json:"name"`
	IsAdmin bool     `json:"is_admin"`
	Modules []string `json:"modules"`
}

func (id *platformIdentity) hasModule(key string) bool {
	if id.IsAdmin {
		return true
	}
	for _, m := range id.Modules {
		if m == key {
			return true
		}
	}
	return false
}

type ssoCacheEntry struct {
	id      platformIdentity
	expires time.Time
}

// SSOBridge valida sesiones de la plataforma y provisiona usuarios locales.
type SSOBridge struct {
	cfg    SSOConfig
	auth   *services.AuthService
	client *http.Client

	mu    sync.Mutex
	cache map[string]ssoCacheEntry
}

func NewSSOBridge(cfg SSOConfig, auth *services.AuthService) *SSOBridge {
	return &SSOBridge{
		cfg:    cfg,
		auth:   auth,
		client: &http.Client{Timeout: 5 * time.Second},
		cache:  make(map[string]ssoCacheEntry),
	}
}

// Enabled reports whether the SSO bridge is configured. When disabled, the
// handlers fall back to the local username/password session flow.
func (b *SSOBridge) Enabled() bool {
	return b != nil && b.cfg.IntrospectURL != "" && b.cfg.SharedSecret != ""
}

// LoginURL is the platform login target (for redirecting unauthenticated users).
func (b *SSOBridge) LoginURL() string { return b.cfg.LoginURL }

// LogoutURL is the platform logout target (empty => fall back to local /login).
func (b *SSOBridge) LogoutURL() string { return b.cfg.LogoutURL }

func (b *SSOBridge) introspect(ctx context.Context, token string) (*platformIdentity, error) {
	b.mu.Lock()
	if e, ok := b.cache[token]; ok && time.Now().Before(e.expires) {
		id := e.id
		b.mu.Unlock()
		return &id, nil
	}
	b.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		b.cfg.IntrospectURL+"?token="+url.QueryEscape(token), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-SSO-Secret", b.cfg.SharedSecret)

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("introspect status %d", resp.StatusCode)
	}
	var id platformIdentity
	if err := json.NewDecoder(resp.Body).Decode(&id); err != nil {
		return nil, err
	}

	b.mu.Lock()
	b.cache[token] = ssoCacheEntry{id: id, expires: time.Now().Add(ssoCacheTTL)}
	b.mu.Unlock()
	return &id, nil
}

// Authenticate resolves the request's platform session to a local inventory user.
// On success returns (user, true). On failure it writes the response (redirect to
// the platform login, or 403/500) and returns (nil, false).
func (b *SSOBridge) Authenticate(w http.ResponseWriter, r *http.Request) (*models.User, bool) {
	cookie, err := r.Cookie(platformSessionCookie)
	if err != nil || cookie.Value == "" {
		b.redirectToLogin(w, r)
		return nil, false
	}

	id, err := b.introspect(r.Context(), cookie.Value)
	if err != nil {
		slog.Warn("SSO introspect failed", "error", err)
		b.redirectToLogin(w, r)
		return nil, false
	}

	if !id.hasModule(b.cfg.RequiredModule) {
		http.Error(w, "No tenés acceso al módulo Inventario. Pedile acceso a un administrador.", http.StatusForbidden)
		return nil, false
	}

	user, err := b.auth.EnsureSSOUser(r.Context(), id.Email, id.Name, id.IsAdmin)
	if err != nil {
		slog.Error("SSO user provisioning failed", "error", err, "email", id.Email)
		http.Error(w, "Error interno de autenticación", http.StatusInternalServerError)
		return nil, false
	}
	return user, true
}

func (b *SSOBridge) redirectToLogin(w http.ResponseWriter, r *http.Request) {
	if b.cfg.LoginURL == "" {
		http.Error(w, "No autenticado", http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, b.cfg.LoginURL, http.StatusSeeOther)
}
