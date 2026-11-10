package importer

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// Record es una fila de procedimiento realizado lista para insertar.
type Record struct {
	MRN         string
	Study       string
	VisitName   string
	Procedure   string
	Coordinator string // puede quedar vacío
	Date        string // ISO YYYY-MM-DD
}

// Índices de columna según el encabezado del export del CTMS.
const (
	colMRN = iota
	colStudy
	colVisit
	colProcedure
	colCoordinator
	colDate
	nCols
)

// parseXLSX lee un .xlsx (desde un reader) y devuelve los procedimientos
// realizados (con fecha). Reporta cuántas filas de datos se leyeron en total
// (rowsRead) para poder contar las descartadas.
func parseXLSX(r io.Reader) (records []Record, rowsRead int, err error) {
	f, err := excelize.OpenReader(r)
	if err != nil {
		return nil, 0, fmt.Errorf("abrir xlsx: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, 0, fmt.Errorf("xlsx sin hojas")
	}
	rows, err := f.Rows(sheets[0])
	if err != nil {
		return nil, 0, fmt.Errorf("leer filas: %w", err)
	}
	defer rows.Close()

	headerFound := false
	for rows.Next() {
		cols, err := rows.Columns()
		if err != nil {
			return nil, rowsRead, fmt.Errorf("leer columnas: %w", err)
		}
		if len(cols) == 0 {
			continue
		}

		// Localizar dinámicamente la fila de encabezado (primera celda == "MRN").
		if !headerFound {
			if strings.EqualFold(strings.TrimSpace(cols[0]), "MRN") {
				headerFound = true
			}
			continue
		}

		// Fila de datos.
		get := func(i int) string {
			if i < len(cols) {
				return strings.TrimSpace(cols[i])
			}
			return ""
		}
		mrn := get(colMRN)
		study := get(colStudy)
		visit := get(colVisit)
		proc := get(colProcedure)
		// Fila totalmente vacía → ignorar (no cuenta como dato).
		if mrn == "" && study == "" && visit == "" && proc == "" {
			continue
		}
		rowsRead++

		isoDate, ok := toISODate(get(colDate))
		if !ok {
			// Sin fecha => visita planificada / no realizada => se descarta.
			continue
		}
		records = append(records, Record{
			MRN:         mrn,
			Study:       study,
			VisitName:   visit,
			Procedure:   proc,
			Coordinator: get(colCoordinator),
			Date:        isoDate,
		})
	}
	if err := rows.Error(); err != nil {
		return nil, rowsRead, err
	}
	if !headerFound {
		return nil, 0, fmt.Errorf("no se encontró la fila de encabezado (MRN)")
	}
	return records, rowsRead, nil
}

// toISODate convierte el valor de la celda de fecha a ISO (YYYY-MM-DD).
// Acepta el serial de Excel (ej. "45875" o "45875.0") y algunos formatos de
// fecha de texto. Devuelve ok=false si está vacío o no es interpretable.
func toISODate(v string) (string, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	// Serial de Excel.
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		if f <= 0 {
			return "", false
		}
		t, err := excelize.ExcelDateToTime(f, false)
		if err != nil {
			return "", false
		}
		return t.Format("2006-01-02"), true
	}
	// Texto en formatos habituales. El export del CTMS usa "02/Jan/2006".
	layouts := []string{
		"02/Jan/2006", "2/Jan/2006", "02/Jan/2006 15:04:05",
		"2006-01-02", "2006-01-02 15:04:05", "02/01/2006",
		"01/02/2006", "2/1/2006", "2006/01/02",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, v); err == nil {
			return t.Format("2006-01-02"), true
		}
	}
	return "", false
}
