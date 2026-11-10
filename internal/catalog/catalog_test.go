package catalog

import (
	"gruposur.ar/plataforma/internal/auth"
	"testing"
)

func TestComplianceCardAndAccess(t *testing.T) {
	modules := Build("https://estadisticas.gruposur.ar", "https://inventario.gruposur.ar", "https://compliance.gruposur.ar")
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
