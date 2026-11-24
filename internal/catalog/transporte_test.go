package catalog

import (
	"gruposur.ar/plataforma/internal/auth"
	"testing"
)

func TestTransporteCardAndAccess(t *testing.T) {
	for _, base := range []string{"http://localhost:4050", "https://transporte.gruposur.ar"} {
		modules := Build("http://localhost:8080", "http://localhost:4040", "http://localhost:8081", "http://localhost:8083", "http://localhost:4060", base)
		m, ok := modules.ByKey("transporte")
		if !ok || m.Name != "Transporte" || m.URL() != base+"/" || m.Desc != "Gestión de los traslados de los pacientes de los ensayos clínicos hacia y desde las sedes." {
			t.Fatalf("invalid Transporte card: %+v", m)
		}
		for _, tc := range []struct {
			name string
			user *auth.User
			want bool
		}{
			{"anonymous", nil, false},
			{"without module", &auth.User{}, false},
			{"other module", &auth.User{Modules: []string{"inventario", "farmacia_finanzas"}}, false},
			{"member", &auth.User{Modules: []string{"transporte"}}, true},
			{"admin", &auth.User{IsAdmin: true}, true},
		} {
			t.Run(base+"/"+tc.name, func(t *testing.T) {
				_, got := modules.For(tc.user).ByKey("transporte")
				if got != tc.want {
					t.Fatalf("access=%v want=%v", got, tc.want)
				}
			})
		}
		for _, key := range []string{"estadisticas", "inventario", "compliance", "pizarra", "farmacia"} {
			if _, ok := modules.ByKey(key); !ok {
				t.Fatalf("existing module missing: %s", key)
			}
		}
	}
}
