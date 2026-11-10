package importer

import (
	"os"
	"path/filepath"
	"testing"

	"gruposur.ar/plataforma/internal/db"
)

// TestImportRealZip valida el importador contra el export real del CTMS.
// Espera que "Reporte de Visitas.zip" esté en la raíz del repo.
func TestImportRealZip(t *testing.T) {
	zipPath := filepath.Join("..", "..", "Reporte de Visitas.zip")
	zipData, err := os.ReadFile(zipPath)
	if err != nil {
		t.Skipf("no se encontró el zip de prueba: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	sdb, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer sdb.Close()

	res, err := ImportZip(sdb, zipData, "Reporte de Visitas.zip", "test@gruposur.ar")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	t.Logf("archivos=%v total=%d insertados=%d omitidos=%d",
		res.Files, res.RowsTotal, res.RowsInserted, res.RowsSkipped)

	// Esperado del análisis: 23.254 procedimientos realizados.
	if res.RowsInserted != 23254 {
		t.Errorf("procedimientos insertados = %d, esperado 23254", res.RowsInserted)
	}

	// Visitas realizadas distintas (MRN+estudio+visita+fecha) = 4.787.
	var visits int
	if err := sdb.QueryRow(
		`SELECT COUNT(*) FROM (SELECT DISTINCT mrn, study, visit_name, procedure_date FROM visit_procedures)`,
	).Scan(&visits); err != nil {
		t.Fatal(err)
	}
	if visits != 4787 {
		t.Errorf("visitas realizadas = %d, esperado 4787", visits)
	}

	// Reimportar no debe duplicar (dedup por clave natural).
	res2, err := ImportZip(sdb, zipData, "Reporte de Visitas.zip", "test@gruposur.ar")
	if err != nil {
		t.Fatalf("reimport: %v", err)
	}
	if res2.RowsInserted != 0 {
		t.Errorf("reimportar insertó %d filas, esperado 0", res2.RowsInserted)
	}

	var total int
	sdb.QueryRow(`SELECT COUNT(*) FROM visit_procedures`).Scan(&total)
	if total != 23254 {
		t.Errorf("total tras reimportar = %d, esperado 23254", total)
	}
}
