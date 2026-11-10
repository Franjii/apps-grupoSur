package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"inventory-webapp/internal/database"
	"inventory-webapp/internal/models"
	"inventory-webapp/internal/services"
)

// fakePlatform mimics the platform's /auth/introspect endpoint.
func fakePlatform(t *testing.T, secret string, id platformIdentity) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-SSO-Secret") != secret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("token") == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(id)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newSSOBridge(t *testing.T, platformURL, secret string) (*SSOBridge, *services.AuthService) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "inv.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	authSvc := services.NewAuthService(db)
	bridge := NewSSOBridge(SSOConfig{
		IntrospectURL:  platformURL + "/auth/introspect",
		SharedSecret:   secret,
		LoginURL:       "https://login.gruposur.ar/",
		RequiredModule: "inventario",
	}, authSvc)
	return bridge, authSvc
}

func TestSSOAuthenticateProvisionsUser(t *testing.T) {
	platform := fakePlatform(t, "s3cr3t", platformIdentity{
		Email: "coord@gruposur.ar", Name: "Coord Uno", IsAdmin: false,
		Modules: []string{"inventario"},
	})
	bridge, authSvc := newSSOBridge(t, platform.URL, "s3cr3t")

	var seen *models.User
	h := RequireAuth(NewSessionStore(false), authSvc, bridge)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = models.UserFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

	req := httptest.NewRequest(http.MethodGet, "/stock", nil)
	req.AddCookie(&http.Cookie{Name: platformSessionCookie, Value: "any-token"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuve %d (%s)", rr.Code, rr.Body.String())
	}
	if seen == nil || seen.Email == nil || *seen.Email != "coord@gruposur.ar" {
		t.Fatalf("usuario mal provisionado: %+v", seen)
	}
	if seen.Role != models.RoleUser {
		t.Fatalf("esperaba rol user, obtuve %q", seen.Role)
	}

	// Idempotencia: una segunda request debe resolver el MISMO usuario (no duplica).
	seen = nil
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req)
	if rr2.Code != http.StatusOK || seen == nil {
		t.Fatalf("segunda request falló: %d", rr2.Code)
	}
	existing, _ := authSvc.GetUserByEmail(req.Context(), "coord@gruposur.ar")
	if existing == nil {
		t.Fatal("el usuario no persistió")
	}
}

func TestSSOAdminMapsToAdminRole(t *testing.T) {
	platform := fakePlatform(t, "s3cr3t", platformIdentity{
		Email: "boss@gruposur.ar", Name: "Boss", IsAdmin: true, Modules: nil,
	})
	bridge, _ := newSSOBridge(t, platform.URL, "s3cr3t")

	var seen *models.User
	h := RequireAuth(NewSessionStore(false), nil, bridge)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = models.UserFromContext(r.Context())
		}))
	req := httptest.NewRequest(http.MethodGet, "/stock", nil)
	req.AddCookie(&http.Cookie{Name: platformSessionCookie, Value: "tok"})
	h.ServeHTTP(httptest.NewRecorder(), req)

	if seen == nil || !seen.IsAdmin() || seen.Role != models.RoleAdmin {
		t.Fatalf("admin de plataforma no mapeó a admin: %+v", seen)
	}
}

func TestSSODeniesWithoutModule(t *testing.T) {
	platform := fakePlatform(t, "s3cr3t", platformIdentity{
		Email: "nobody@gruposur.ar", IsAdmin: false, Modules: []string{"estadisticas"},
	})
	bridge, authSvc := newSSOBridge(t, platform.URL, "s3cr3t")

	h := RequireAuth(NewSessionStore(false), authSvc, bridge)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("no debería alcanzar el handler protegido")
		}))
	req := httptest.NewRequest(http.MethodGet, "/stock", nil)
	req.AddCookie(&http.Cookie{Name: platformSessionCookie, Value: "tok"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("esperaba 403, obtuve %d", rr.Code)
	}
}

func TestSSORedirectsWithoutCookie(t *testing.T) {
	platform := fakePlatform(t, "s3cr3t", platformIdentity{})
	bridge, authSvc := newSSOBridge(t, platform.URL, "s3cr3t")

	h := RequireAuth(NewSessionStore(false), authSvc, bridge)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("no debería alcanzar el handler protegido")
		}))
	req := httptest.NewRequest(http.MethodGet, "/stock", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("esperaba 303, obtuve %d", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "https://login.gruposur.ar/" {
		t.Fatalf("redirect inesperado: %q", loc)
	}
}
