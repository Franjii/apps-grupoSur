package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"gruposur.ar/plataforma/internal/auth"
	"gruposur.ar/plataforma/internal/db"
)

func newIntrospectServer(t *testing.T, secret string) (*Server, *auth.Store) {
	t.Helper()
	sdb, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Seed(sdb, []string{"admin@gruposur.ar"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sdb.Close() })
	store := auth.NewStore(sdb)
	return &Server{DB: sdb, Store: store, SSOSecret: secret}, store
}

func TestIntrospect(t *testing.T) {
	srv, store := newIntrospectServer(t, "s3cr3t")

	admin, err := store.UserByEmail("admin@gruposur.ar")
	if err != nil {
		t.Fatal(err)
	}
	token, err := store.CreateSession(admin.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	// Caso feliz: secreto correcto + token válido => 200 + JSON del usuario.
	req := httptest.NewRequest(http.MethodGet, "/auth/introspect?token="+token, nil)
	req.Header.Set("X-SSO-Secret", "s3cr3t")
	rr := httptest.NewRecorder()
	srv.introspect(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("esperaba 200, obtuve %d (%s)", rr.Code, rr.Body.String())
	}
	var out struct {
		Email   string   `json:"email"`
		IsAdmin bool     `json:"is_admin"`
		Modules []string `json:"modules"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Email != "admin@gruposur.ar" || !out.IsAdmin {
		t.Fatalf("payload inesperado: %+v", out)
	}
	// El admin fue sembrado con acceso a todos los módulos, incluido inventario.
	if !contains(out.Modules, "inventario") {
		t.Fatalf("esperaba módulo inventario en %v", out.Modules)
	}
}

func TestIntrospectRejects(t *testing.T) {
	srv, store := newIntrospectServer(t, "s3cr3t")
	admin, _ := store.UserByEmail("admin@gruposur.ar")
	token, _ := store.CreateSession(admin.ID, time.Hour)

	cases := []struct {
		name   string
		secret string
		token  string
		want   int
	}{
		{"secreto incorrecto", "wrong", token, http.StatusUnauthorized},
		{"sin secreto", "", token, http.StatusUnauthorized},
		{"token inválido", "s3cr3t", "no-existe", http.StatusUnauthorized},
		{"sin token", "s3cr3t", "", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			url := "/auth/introspect"
			if c.token != "" {
				url += "?token=" + c.token
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			if c.secret != "" {
				req.Header.Set("X-SSO-Secret", c.secret)
			}
			rr := httptest.NewRecorder()
			srv.introspect(rr, req)
			if rr.Code != c.want {
				t.Fatalf("esperaba %d, obtuve %d", c.want, rr.Code)
			}
		})
	}
}

// contains es un helper local para el test.
func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
