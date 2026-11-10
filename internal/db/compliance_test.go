package db

import (
	"path/filepath"
	"testing"
)

func TestComplianceSeedIsAdditiveAndIdempotent(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Seed(db, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(email,name,is_admin,active) VALUES('existing@example.test','Existing',0,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO user_modules(user_id,module_key) SELECT id,'inventario' FROM users WHERE email='existing@example.test'`); err != nil {
		t.Fatal(err)
	}
	if err := Seed(db, nil); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM modules WHERE key='compliance'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("compliance seed=%d %v", count, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_modules WHERE module_key='inventario'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("existing access lost: %d %v", count, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_modules WHERE module_key='compliance'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unrequested access granted: %d %v", count, err)
	}
}
