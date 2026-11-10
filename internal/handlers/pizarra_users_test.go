package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPizarraDirectory(t *testing.T) {
	srv, store := newIntrospectServer(t, "secret")
	for _, u := range []struct {
		email   string
		modules []string
	}{
		{"member@gruposur.ar", []string{"pizarra"}},
		{"other@gruposur.ar", []string{"inventario"}},
		{"inactive@gruposur.ar", []string{"pizarra"}},
	} {
		if err := store.CreateUser(u.email, u.email, false, u.modules); err != nil {
			t.Fatal(err)
		}
	}
	inactive, _ := store.UserByEmail("inactive@gruposur.ar")
	if err := store.SetUserActive(inactive.ID, false); err != nil {
		t.Fatal(err)
	}
	member, _ := store.UserByEmail("member@gruposur.ar")
	token, err := store.CreateSession(member.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	other, _ := store.UserByEmail("other@gruposur.ar")
	otherToken, err := store.CreateSession(other.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		secret, token string
		want          int
	}{
		{"secret", token, 200}, {"wrong", token, 401}, {"secret", "", 401}, {"secret", otherToken, 403},
	} {
		r := httptest.NewRequest("GET", "/auth/pizarra/users", nil)
		r.Header.Set("X-SSO-Secret", tc.secret)
		r.Header.Set("X-Session-Token", tc.token)
		w := httptest.NewRecorder()
		srv.pizarraUsers(w, r)
		if w.Code != tc.want {
			t.Fatalf("status=%d want=%d: %s", w.Code, tc.want, w.Body.String())
		}
		if tc.want == 200 {
			var users []struct {
				Email string `json:"email"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &users); err != nil {
				t.Fatal(err)
			}
			if len(users) != 2 || users[0].Email != "admin@gruposur.ar" || users[1].Email != "member@gruposur.ar" {
				t.Fatalf("unexpected directory: %+v", users)
			}
		}
	}
	if err := store.SetUserModules(member.ID, []string{}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/auth/pizarra/users", nil)
	r.Header.Set("X-SSO-Secret", "secret")
	r.Header.Set("X-Session-Token", token)
	w := httptest.NewRecorder()
	srv.pizarraUsers(w, r)
	if w.Code != 403 {
		t.Fatalf("revoked module: %d", w.Code)
	}
}
