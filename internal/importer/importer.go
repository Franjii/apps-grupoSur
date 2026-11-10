// Package importer procesa los .zip exportados del CTMS (RealTime-CTMS),
// que contienen uno o varios .xlsx, e inserta los procedimientos realizados.
package importer

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// Result resume el resultado de una importación.
type Result struct {
	BatchID      int64
	Files        []string
	RowsTotal    int // filas de datos leídas en todos los xlsx
	RowsInserted int // procedimientos realizados nuevos
	RowsSkipped  int // planificadas (sin fecha) + duplicadas
}

// ImportZip lee el contenido de un .zip en memoria y carga los procedimientos
// realizados en la base, deduplicando por la clave natural.
func ImportZip(sdb *sql.DB, zipData []byte, filename, importedBy string) (*Result, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("abrir zip: %w", err)
	}

	// Tomar las entradas .xlsx (ignorar carpetas, archivos de macOS, etc.).
	var entries []*zip.File
	for _, f := range zr.File {
		name := f.Name
		if f.FileInfo().IsDir() ||
			strings.HasPrefix(name, "__MACOSX") ||
			!strings.HasSuffix(strings.ToLower(name), ".xlsx") {
			continue
		}
		entries = append(entries, f)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("el .zip no contiene archivos .xlsx")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	// Parsear todos los xlsx antes de tocar la base.
	var allRecords []Record
	var fileNames []string
	rowsTotal := 0
	for _, e := range entries {
		rc, err := e.Open()
		if err != nil {
			return nil, fmt.Errorf("abrir %s: %w", e.Name, err)
		}
		recs, read, err := parseXLSX(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("procesar %s: %w", e.Name, err)
		}
		allRecords = append(allRecords, recs...)
		rowsTotal += read
		fileNames = append(fileNames, e.Name)
	}

	// Registrar el batch y hacer los inserts en una transacción.
	tx, err := sdb.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(
		`INSERT INTO import_batches(filename, imported_by, rows_total) VALUES (?,?,?)`,
		filename, importedBy, rowsTotal)
	if err != nil {
		return nil, fmt.Errorf("crear batch: %w", err)
	}
	batchID, _ := res.LastInsertId()

	stmt, err := tx.Prepare(`
		INSERT INTO visit_procedures
			(mrn, study, visit_name, procedure, coordinator, procedure_date, import_batch_id)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT (mrn, study, visit_name, procedure, procedure_date) DO NOTHING`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	inserted := 0
	for _, r := range allRecords {
		var coord any
		if r.Coordinator != "" {
			coord = r.Coordinator
		}
		out, err := stmt.Exec(r.MRN, r.Study, r.VisitName, r.Procedure, coord, r.Date, batchID)
		if err != nil {
			return nil, fmt.Errorf("insertar visita: %w", err)
		}
		if n, _ := out.RowsAffected(); n > 0 {
			inserted++
		}
	}

	skipped := rowsTotal - inserted
	if _, err := tx.Exec(
		`UPDATE import_batches SET rows_inserted=?, rows_skipped=? WHERE id=?`,
		inserted, skipped, batchID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &Result{
		BatchID:      batchID,
		Files:        fileNames,
		RowsTotal:    rowsTotal,
		RowsInserted: inserted,
		RowsSkipped:  skipped,
	}, nil
}
