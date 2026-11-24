package config

import (
	"os"
	"testing"
)

func TestTransporteURL(t *testing.T) {
	t.Setenv("TRANSPORTE_BASE_URL", "")
	if err := os.Unsetenv("TRANSPORTE_BASE_URL"); err != nil {
		t.Fatal(err)
	}
	if got := Load().TransporteBaseURL; got != "http://localhost:4050" {
		t.Fatalf("local URL=%q", got)
	}
	t.Setenv("TRANSPORTE_BASE_URL", "https://transporte.gruposur.ar/")
	if got := Load().TransporteBaseURL; got != "https://transporte.gruposur.ar" {
		t.Fatalf("production URL=%q", got)
	}
}
