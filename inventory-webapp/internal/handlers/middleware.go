package handlers

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"inventory-webapp/internal/models"
	"inventory-webapp/internal/services"
)

// UserFromContext extracts the authenticated user from the request context.
// This is a convenience re-export of models.UserFromContext for use within the handlers package.
func UserFromContext(ctx context.Context) *models.User {
	return models.UserFromContext(ctx)
}

const (
	sessionCookieName = "session_id"
	sessionMaxAge     = 8 * 60 * 60 // 8 hours
)

const (
	csrfCookieName = "csrf_token"
	csrfHeaderName = "X-CSRF-Token"
	csrfFormField  = "csrf_token"
)

type sessionEntry struct {
	userID    string
	createdAt time.Time
}

// SessionStore manages user sessions in memory.
type SessionStore struct {
	mu           sync.RWMutex
	sessions     map[string]sessionEntry // sessionID -> session entry
	SecureCookie bool                    // set Secure flag on cookies (true when APP_URL is https)
}

func NewSessionStore(secureCookie bool) *SessionStore {
	return &SessionStore{
		sessions:     make(map[string]sessionEntry),
		SecureCookie: secureCookie,
	}
}

// Create generates a new session for the given user ID and returns the session ID.
func (s *SessionStore) Create(userID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating session ID: %w", err)
	}
	sessionID := hex.EncodeToString(b)

	s.mu.Lock()
	s.sessions[sessionID] = sessionEntry{
		userID:    userID,
		createdAt: time.Now(),
	}
	s.mu.Unlock()

	return sessionID, nil
}

// Get returns the user ID for a session, or false if not found or expired.
func (s *SessionStore) Get(sessionID string) (string, bool) {
	s.mu.RLock()
	entry, ok := s.sessions[sessionID]
	s.mu.RUnlock()
	if !ok {
		return "", false
	}

	if time.Now().After(entry.createdAt.Add(time.Duration(sessionMaxAge) * time.Second)) {
		s.mu.Lock()
		delete(s.sessions, sessionID)
		s.mu.Unlock()
		return "", false
	}

	return entry.userID, true
}

// Delete removes a session.
func (s *SessionStore) Delete(sessionID string) {
	s.mu.Lock()
	delete(s.sessions, sessionID)
	s.mu.Unlock()
}

// DeleteByUserID removes all sessions for the given user ID.
func (s *SessionStore) DeleteByUserID(userID string) {
	s.mu.Lock()
	for id, entry := range s.sessions {
		if entry.userID == userID {
			delete(s.sessions, id)
		}
	}
	s.mu.Unlock()
}

// Cleanup removes all expired sessions.
func (s *SessionStore) Cleanup() {
	now := time.Now()
	maxAge := time.Duration(sessionMaxAge) * time.Second

	s.mu.Lock()
	for id, entry := range s.sessions {
		if now.After(entry.createdAt.Add(maxAge)) {
			delete(s.sessions, id)
		}
	}
	s.mu.Unlock()
}

// StartCleanup runs Cleanup on a recurring interval in a background goroutine.
// It stops when the provided context is cancelled.
func (s *SessionStore) StartCleanup(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("Session cleanup stopped")
				return
			case <-ticker.C:
				s.Cleanup()
			}
		}
	}()
}

func clearSessionCookie(w http.ResponseWriter, secureCookie bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearCSRFCookie(w http.ResponseWriter, secureCookie bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

// RequireAuth returns middleware that ensures the request has a valid session.
// When the SSO bridge is enabled, identity comes from the platform's gs_session
// cookie (single sign-on); otherwise it falls back to the local session cookie.
// If not authenticated, redirects to the login (platform or local).
func RequireAuth(sessions *SessionStore, authService *services.AuthService, sso *SSOBridge) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if sso.Enabled() {
				user, ok := sso.Authenticate(w, r)
				if !ok {
					return // sso ya escribió la respuesta (redirect/403/500)
				}
				ctx := models.ContextWithUser(r.Context(), user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			cookie, err := r.Cookie(sessionCookieName)
			if err != nil {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			userID, ok := sessions.Get(cookie.Value)
			if !ok {
				clearSessionCookie(w, sessions.SecureCookie)
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			user, err := authService.GetActiveUserSafe(r.Context(), userID)
			if err != nil {
				sessions.Delete(cookie.Value)
				clearSessionCookie(w, sessions.SecureCookie)
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			ctx := models.ContextWithUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CSRFProtect returns middleware that implements double-submit cookie CSRF protection.
func CSRFProtect(secureCookie bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var token string
			cookie, err := r.Cookie(csrfCookieName)
			if err != nil || cookie.Value == "" {
				var genErr error
				token, genErr = generateCSRFToken()
				if genErr != nil {
					slog.Error("failed to generate CSRF token", "error", genErr)
					http.Error(w, "Error interno", http.StatusInternalServerError)
					return
				}
				http.SetCookie(w, &http.Cookie{
					Name:     csrfCookieName,
					Value:    token,
					Path:     "/",
					MaxAge:   sessionMaxAge,
					HttpOnly: true,
					Secure:   secureCookie,
					SameSite: http.SameSiteLaxMode,
				})
			} else {
				token = cookie.Value
			}

			ctx := models.ContextWithCSRFToken(r.Context(), token)
			r = r.WithContext(ctx)

			if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
				submitted := r.Header.Get(csrfHeaderName)
				if submitted == "" {
					if parseErr := r.ParseForm(); parseErr == nil {
						submitted = r.FormValue(csrfFormField)
					}
				}

				if !validateCSRFToken(token, submitted) {
					slog.Warn("CSRF validation failed", "method", r.Method, "path", r.URL.Path)
					http.Error(w, "Forbidden", http.StatusForbidden)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func generateCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating CSRF token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func validateCSRFToken(expected, submitted string) bool {
	if expected == "" || submitted == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(submitted)) == 1
}

// MaxBodySize returns middleware that limits the size of request bodies.
func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}
