// Package visits contiene la lógica de consulta del dashboard de Visitas.
package visits

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"
)

// Filters son los filtros del dashboard. Dentro de cada lista la semántica es
// OR; entre filtros distintos es AND. Rango de fecha inclusivo (ISO).
type Filters struct {
	Studies      []string
	Visits       []string
	Procedures   []string
	Coordinators []string
	DateFrom     string
	DateTo       string
}

// ── Facetas ──────────────────────────────────────────────────────────────
//
// FUENTE ÚNICA de las dimensiones filtrables. Sumar una faceta = agregar una
// fila acá + su campo en Filters y en Options (+ su índice en schema.sql). El
// resto (WHERE, faceted search, parseo, plantillas) se deriva de esta tabla.

// facet describe una dimensión filtrable y cómo acceder a ella en Filters/Options.
type facet struct {
	Name, Column, Label, ID string                   // nombre HTTP, columna SQL, etiqueta, id de DOM
	sel                     func(*Filters) *[]string // accesor a la selección
	opt                     func(*Options) *[]string // accesor a las opciones
}

var facets = []facet{
	{"study", "study", "Estudio", "f-study",
		func(f *Filters) *[]string { return &f.Studies },
		func(o *Options) *[]string { return &o.Studies }},
	{"visit", "visit_name", "Visita", "f-visit",
		func(f *Filters) *[]string { return &f.Visits },
		func(o *Options) *[]string { return &o.Visits }},
	{"procedure", "procedure", "Procedimiento", "f-procedure",
		func(f *Filters) *[]string { return &f.Procedures },
		func(o *Options) *[]string { return &o.Procedures }},
	{"coordinator", "coordinator", "Profesional", "f-coordinator",
		func(f *Filters) *[]string { return &f.Coordinators },
		func(o *Options) *[]string { return &o.Coordinators }},
}

// Facet es la vista pública de una faceta (para armar la UI, sin exponer accesores).
type Facet struct{ Name, Label, ID string }

// Facets son las facetas en orden de presentación (derivadas de la tabla interna).
var Facets []Facet

func init() {
	Facets = make([]Facet, len(facets))
	for i, d := range facets {
		Facets[i] = Facet{Name: d.Name, Label: d.Label, ID: d.ID}
	}
}

// ParseQuery arma los filtros desde la query HTTP, limpiando espacios y vacíos.
func ParseQuery(q url.Values) Filters {
	var f Filters
	for _, d := range facets {
		*d.sel(&f) = cleanValues(q[d.Name])
	}
	f.DateFrom = strings.TrimSpace(q.Get("from"))
	f.DateTo = strings.TrimSpace(q.Get("to"))
	return f
}

// Values devuelve la selección de la faceta con ese nombre HTTP (nil si no existe).
func (f Filters) Values(name string) []string {
	for _, d := range facets {
		if d.Name == name {
			return *d.sel(&f)
		}
	}
	return nil
}

// Values devuelve las opciones de la faceta con ese nombre HTTP (nil si no existe).
func (o Options) Values(name string) []string {
	for _, d := range facets {
		if d.Name == name {
			return *d.opt(&o)
		}
	}
	return nil
}

func cleanValues(vs []string) []string {
	var out []string
	for _, v := range vs {
		if t := strings.TrimSpace(v); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// clave de "visita realizada" distinta dentro de SQL.
const visitKey = `mrn||char(31)||study||char(31)||visit_name||char(31)||procedure_date`

// where construye la cláusula WHERE y sus argumentos a partir de los filtros.
func (f Filters) where() (string, []any) {
	var clauses []string
	var args []any

	inList := func(col string, vals []string) {
		if len(vals) == 0 {
			return
		}
		ph := make([]string, len(vals))
		for i, v := range vals {
			ph[i] = "?"
			args = append(args, v)
		}
		clauses = append(clauses, fmt.Sprintf("%s IN (%s)", col, strings.Join(ph, ",")))
	}

	for _, d := range facets {
		inList(d.Column, *d.sel(&f))
	}
	if f.DateFrom != "" {
		clauses = append(clauses, "procedure_date >= ?")
		args = append(args, f.DateFrom)
	}
	if f.DateTo != "" {
		clauses = append(clauses, "procedure_date <= ?")
		args = append(args, f.DateTo)
	}

	if len(clauses) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(clauses, " AND "), args
}

// Hero son las métricas del componente Hero (respetan los filtros).
type Hero struct {
	Visits     int // visitas realizadas distintas
	VisitTypes int // tipos de visita distintos
	Studies    int // estudios distintos
}

func GetHero(db *sql.DB, f Filters) (Hero, error) {
	w, args := f.where()
	q := fmt.Sprintf(`
		SELECT
			COUNT(DISTINCT %s),
			COUNT(DISTINCT visit_name),
			COUNT(DISTINCT study)
		FROM visit_procedures %s`, visitKey, w)
	var h Hero
	err := db.QueryRow(q, args...).Scan(&h.Visits, &h.VisitTypes, &h.Studies)
	return h, err
}

// Bar es una barra del gráfico (par estudio+visita).
type Bar struct {
	Study string
	Visit string
	Label string
	Count int
}

// GetChart devuelve las barras (visitas realizadas por estudio+visita),
// ordenadas de mayor a menor. limit<=0 trae todas.
func GetChart(db *sql.DB, f Filters, limit int) ([]Bar, error) {
	w, args := f.where()
	q := fmt.Sprintf(`
		SELECT study, visit_name, COUNT(DISTINCT mrn||char(31)||procedure_date) AS c
		FROM visit_procedures %s
		GROUP BY study, visit_name
		ORDER BY c DESC, study, visit_name`, w)
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bars []Bar
	for rows.Next() {
		var b Bar
		if err := rows.Scan(&b.Study, &b.Visit, &b.Count); err != nil {
			return nil, err
		}
		b.Label = b.Study + " - " + b.Visit
		bars = append(bars, b)
	}
	return bars, rows.Err()
}

// CountGroups cuenta cuántos pares estudio+visita hay para los filtros dados.
func CountGroups(db *sql.DB, f Filters) (int, error) {
	w, args := f.where()
	q := fmt.Sprintf(`
		SELECT COUNT(*) FROM (
			SELECT 1 FROM visit_procedures %s GROUP BY study, visit_name
		)`, w)
	var n int
	err := db.QueryRow(q, args...).Scan(&n)
	return n, err
}

// Options son los valores disponibles para poblar los filtros.
type Options struct {
	Studies      []string
	Visits       []string
	Procedures   []string
	Coordinators []string
	MinDate      string
	MaxDate      string
}

// GetFacetedOptions devuelve las opciones disponibles de cada filtro teniendo en
// cuenta los demás filtros activos (faceted search). Para cada filtro se aplican
// todos los filtros EXCEPTO el propio, de modo que se puedan seguir agregando
// valores dentro de ese filtro. Ej.: con dos estudios seleccionados, la lista de
// Visitas solo muestra las visitas de esos estudios.
func GetFacetedOptions(db *sql.DB, f Filters) (Options, error) {
	var o Options

	// Cada faceta excluye su propia selección para poder seguir sumando valores.
	for _, d := range facets {
		fs := f
		*d.sel(&fs) = nil
		vals, err := distinctFiltered(db, d.Column, fs)
		if err != nil {
			return o, err
		}
		*d.opt(&o) = vals
	}

	// Rango de fechas disponible según los demás filtros (excluye el de fecha).
	fsDate := f
	fsDate.DateFrom, fsDate.DateTo = "", ""
	w, args := fsDate.where()
	q := "SELECT COALESCE(MIN(procedure_date),''), COALESCE(MAX(procedure_date),'') FROM visit_procedures " + w
	if err := db.QueryRow(q, args...).Scan(&o.MinDate, &o.MaxDate); err != nil {
		return o, err
	}
	return o, nil
}

// distinctFiltered devuelve los valores distintos de una columna aplicando los
// filtros dados. col proviene de una lista fija interna (no de entrada de usuario).
func distinctFiltered(db *sql.DB, col string, f Filters) ([]string, error) {
	w, args := f.where()
	cond := fmt.Sprintf("%s IS NOT NULL AND %s <> ''", col, col)
	if w == "" {
		w = "WHERE " + cond
	} else {
		w = w + " AND " + cond
	}
	q := fmt.Sprintf("SELECT DISTINCT %s FROM visit_procedures %s ORDER BY %s", col, w, col)
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
