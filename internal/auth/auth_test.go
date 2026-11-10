package auth

import (
	"path/filepath"
	"testing"
	"time"

	"gruposur.ar/plataforma/internal/db"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	sdb, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Seed(sdb, []string{"admin@gruposur.ar"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sdb.Close() })
	return NewStore(sdb)
}

func TestRBACAndSessions(t *testing.T) {
	s := newTestStore(t)

	// Admin sembrado tiene el módulo visitas.
	admin, err := s.UserByEmail("admin@gruposur.ar")
	if err != nil {
		t.Fatal(err)
	}
	if !admin.IsAdmin || !admin.HasModule("estadisticas") {
		t.Fatalf("admin mal configurado: %+v", admin)
	}

	// Usuario común sin módulos.
	if err := s.CreateUser("coord@gruposur.ar", "Coord", false, nil); err != nil {
		t.Fatal(err)
	}
	u, _ := s.UserByEmail("coord@gruposur.ar")
	if u.HasModule("estadisticas") {
		t.Error("coord no debería tener acceso a estadisticas")
	}

	// Asignar módulo estadisticas.
	if err := s.SetUserModules(u.ID, []string{"estadisticas"}); err != nil {
		t.Fatal(err)
	}
	u, _ = s.UserByEmail("coord@gruposur.ar")
	if !u.HasModule("estadisticas") {
		t.Error("coord debería tener acceso a estadisticas tras asignarlo")
	}

	// Sesión válida resuelve al usuario.
	tok, err := s.CreateSession(u.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.UserBySession(tok)
	if err != nil || got.ID != u.ID {
		t.Fatalf("sesión no resolvió: %v", err)
	}

	// Inhabilitar borra sesiones y bloquea acceso.
	if err := s.SetUserActive(u.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserBySession(tok); err == nil {
		t.Error("sesión debería invalidarse al inhabilitar al usuario")
	}

	// Sesión expirada no resuelve.
	tok2, _ := s.CreateSession(admin.ID, -time.Minute)
	if _, err := s.UserBySession(tok2); err == nil {
		t.Error("sesión expirada no debería resolver")
	}
}
