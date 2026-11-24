package catalog

import (
	"gruposur.ar/plataforma/internal/auth"
	"testing"
)

func TestComplianceCardAndAccess(t *testing.T) {
	modules := Build("https://estadisticas.gruposur.ar", "https://inventario.gruposur.ar", "https://compliance.gruposur.ar", "http://localhost:8081", "http://localhost:4060", "http://localhost:4050")
	module, ok := modules.ByKey("compliance")
	if !ok || module.Name != "Compliance" || module.URL() != "https://compliance.gruposur.ar/" {
		t.Fatalf("invalid card: %+v", module)
	}
	for _, tc := range []struct {
		user *auth.User
		want bool
	}{
		{nil, false},
		{&auth.User{Modules: []string{"inventario"}}, false},
		{&auth.User{Modules: []string{"compliance"}}, true},
		{&auth.User{IsAdmin: true}, true},
	} {
		_, ok := modules.For(tc.user).ByKey("compliance")
		if ok != tc.want {
			t.Fatalf("access=%v want=%v", ok, tc.want)
		}
	}
}

func TestFarmaciaCardAndAccess(t *testing.T) {
	modules := Build("http://localhost:8080", "http://localhost:4040", "http://localhost:8081", "http://localhost:8083", "http://localhost:4060", "http://localhost:4050")
	for _, tc := range []struct {
		name string
		user *auth.User
		want int
	}{
		{"anonymous", nil, 0},
		{"without permissions", &auth.User{}, 0},
		{"other module", &auth.User{Modules: []string{"inventario"}}, 0},
		{"pharmacy", &auth.User{Modules: []string{"farmacia"}}, 1},
		{"finance", &auth.User{Modules: []string{"farmacia_finanzas"}}, 1},
		{"both", &auth.User{Modules: []string{"farmacia", "farmacia_finanzas"}}, 1},
		{"admin", &auth.User{IsAdmin: true}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := 0
			for _, m := range modules.For(tc.user) {
				if m.Key == "farmacia" {
					count++
					if m.Name != "Farmacia" || m.URL() != "http://localhost:4060/" {
						t.Fatalf("invalid card: %+v", m)
					}
				}
				if m.Key == "farmacia_finanzas" {
					t.Fatal("duplicate finance card")
				}
			}
			if count != tc.want {
				t.Fatalf("cards=%d want=%d", count, tc.want)
			}
		})
	}
}
