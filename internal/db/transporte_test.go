package db

import (
	"path/filepath"
	"testing"
)

func TestTransporteSeedPreservesExistingData(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Representa una base anterior a Transporte, con usuario, permiso y sesión.
	for _, query := range []string{
		`INSERT INTO modules(key,name) VALUES ('inventario','Inventario')`,
		`INSERT INTO users(id,email,name,is_admin,active) VALUES (1,'member@example.invalid','Original',0,1)`,
		`INSERT INTO user_modules(user_id,module_key) VALUES (1,'inventario')`,
		`INSERT INTO sessions(id,user_id,expires_at) VALUES ('test-session',1,'2099-01-01T00:00:00Z')`,
		`INSERT INTO visit_procedures(mrn,study,visit_name,procedure,procedure_date) VALUES ('test','study','visit','procedure','2026-01-01')`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := Seed(db, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		query string
		want  int
	}{
		{`SELECT count(*) FROM modules WHERE key='transporte' AND name='Transporte'`, 1},
		{`SELECT count(*) FROM user_modules WHERE user_id=1 AND module_key='transporte'`, 0},
		{`SELECT count(*) FROM user_modules WHERE user_id=1 AND module_key='inventario'`, 1},
		{`SELECT count(*) FROM users WHERE id=1 AND name='Original' AND is_admin=0 AND active=1`, 1},
		{`SELECT count(*) FROM sessions WHERE id='test-session' AND user_id=1 AND expires_at='2099-01-01T00:00:00Z'`, 1},
		{`SELECT count(*) FROM visit_procedures WHERE mrn='test'`, 1},
	} {
		var count int
		if err := db.QueryRow(tc.query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != tc.want {
			t.Fatalf("count=%d want=%d: %s", count, tc.want, tc.query)
		}
	}
	for _, key := range []string{"estadisticas", "inventario", "compliance", "pizarra", "farmacia", "farmacia_finanzas"} {
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM modules WHERE key=?`, key).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("existing module missing: %s", key)
		}
	}
}
