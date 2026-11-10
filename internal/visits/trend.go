package visits

import (
	"database/sql"
	"fmt"
	"html"
	"html/template"
	"strings"
	"time"
)

// TrendPoint es un punto de la serie temporal (un período agregado).
type TrendPoint struct {
	Label string // etiqueta lista para el eje X
	Count int    // visitas realizadas distintas en el período
}

// Vistas válidas de la tendencia.
const (
	TrendDiario    = "diario"
	TrendSemanal   = "semanal"
	TrendMensual   = "mensual"
	TrendHistorico = "historico"
)

// mesesAbrev son las abreviaturas de mes en español (índice 1..12).
var mesesAbrev = [...]string{"", "ene", "feb", "mar", "abr", "may", "jun",
	"jul", "ago", "sep", "oct", "nov", "dic"}

// GetTrend devuelve la serie temporal de visitas realizadas para la vista dada,
// respetando los filtros. El rango de las vistas "de ventana" (semanal, mensual,
// semestral) se ancla al último dato filtrado, no a la fecha actual.
func GetTrend(db *sql.DB, f Filters, view string) ([]TrendPoint, error) {
	w, args := f.where()

	// Ancla: última fecha con datos según los filtros.
	anchorQ := "SELECT COALESCE(MAX(procedure_date),'') FROM visit_procedures " + w
	var anchorStr string
	if err := db.QueryRow(anchorQ, args...).Scan(&anchorStr); err != nil {
		return nil, err
	}
	if anchorStr == "" {
		return nil, nil // sin datos para estos filtros
	}
	anchor, err := time.Parse("2006-01-02", anchorStr)
	if err != nil {
		return nil, fmt.Errorf("ancla inválida %q: %w", anchorStr, err)
	}

	bucket, since, until := bucketSpec(view, anchor)

	// Consulta agrupada por período.
	qArgs := append([]any(nil), args...)
	cond := w
	add := func(clause, val string) {
		if cond == "" {
			cond = "WHERE " + clause
		} else {
			cond += " AND " + clause
		}
		qArgs = append(qArgs, val)
	}
	if since != "" {
		add("procedure_date >= ?", since)
	}
	if until != "" {
		add("procedure_date < ?", until)
	}
	q := fmt.Sprintf(`
		SELECT %s AS b, COUNT(DISTINCT %s) AS c
		FROM visit_procedures %s
		GROUP BY b ORDER BY b`, bucket, visitKey, cond)

	rows, err := db.Query(q, qArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pts []TrendPoint
	for rows.Next() {
		var key string
		var c int
		if err := rows.Scan(&key, &c); err != nil {
			return nil, err
		}
		pts = append(pts, TrendPoint{Label: trendLabel(view, key), Count: c})
	}
	return pts, rows.Err()
}

// bucketSpec devuelve la expresión SQL de bucket, la fecha ISO de inicio de
// ventana (o "" si la vista abarca todo el rango) y la fecha ISO de tope
// exclusivo (o "" si no hay tope) para la vista y el ancla dados.
//
// En las vistas mensual e histórica el tope excluye el mes en curso cuando aún
// no está "cumplido" (el ancla —última fecha del reporte— no llega al último día
// del mes), para que el gráfico no muestre el desplome de un mes parcial.
func bucketSpec(view string, anchor time.Time) (bucket, since, until string) {
	const iso = "2006-01-02"
	switch view {
	case TrendDiario:
		// Bucket = el día (procedure_date ya es una fecha ISO).
		bucket = `procedure_date`
		// Ventana: últimos 30 días respecto del ancla.
		since = anchor.AddDate(0, 0, -29).Format(iso)
	case TrendSemanal:
		// Bucket = lunes de la semana del procedimiento.
		bucket = `date(procedure_date,'-'||((CAST(strftime('%w',procedure_date) AS INTEGER)+6)%7)||' days')`
		// Ventana: lunes de la semana del ancla − 11 semanas (≈12 puntos).
		monday := anchor.AddDate(0, 0, -((int(anchor.Weekday()) + 6) % 7))
		since = monday.AddDate(0, 0, -7*11).Format(iso)
	case TrendMensual:
		bucket = `strftime('%Y-%m',procedure_date)`
		// Último mes cumplido y ventana de 12 puntos que termina en él.
		last := lastCompleteMonth(anchor)
		since = last.AddDate(0, -11, 0).Format(iso)
		until = last.AddDate(0, 1, 0).Format(iso) // exclusivo: excluye meses incompletos
	default: // TrendHistorico
		bucket = `strftime('%Y-%m',procedure_date)`
		// Todo el rango, salvo el mes en curso si está incompleto.
		until = lastCompleteMonth(anchor).AddDate(0, 1, 0).Format(iso)
	}
	return bucket, since, until
}

// lastCompleteMonth devuelve el primer día del último mes "cumplido" respecto
// del ancla: si el ancla es el último día de su mes, ese mes está completo; en
// caso contrario se toma el mes anterior.
func lastCompleteMonth(anchor time.Time) time.Time {
	som := time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, time.UTC)
	if anchor.AddDate(0, 0, 1).Month() != anchor.Month() {
		return som // el ancla cae en el último día del mes → mes cumplido
	}
	return som.AddDate(0, -1, 0) // mes en curso incompleto → mes anterior
}

// trendLabel formatea la clave de bucket a una etiqueta legible según la vista.
func trendLabel(view, key string) string {
	switch view {
	case TrendDiario, TrendSemanal: // key = "2006-01-02" -> "dd/MM"
		if t, err := time.Parse("2006-01-02", key); err == nil {
			return t.Format("02/01")
		}
	case TrendMensual, TrendHistorico: // key = "2006-01" -> "abr '25"
		if y, m, ok := splitYearMonth(key); ok {
			return fmt.Sprintf("%s '%02d", mesesAbrev[m], y%100)
		}
	}
	return key // fallback
}

func splitYearMonth(key string) (year, month int, ok bool) {
	if len(key) != 7 || key[4] != '-' {
		return 0, 0, false
	}
	var y, m int
	if _, err := fmt.Sscanf(key, "%d-%d", &y, &m); err != nil || m < 1 || m > 12 {
		return 0, 0, false
	}
	return y, m, true
}

// RenderTrendSVG dibuja una serie temporal (ya ordenada por tiempo) como línea
// con relleno de área, en SVG server-side y sin JS.
func RenderTrendSVG(points []TrendPoint) template.HTML {
	if len(points) == 0 {
		return emptyChart
	}

	const (
		w    = 980 // ancho del viewBox
		h    = 300 // alto del viewBox
		padL = 44  // margen izq. (eje Y)
		padR = 16
		padT = 16
		padB = 40 // margen inf. (etiquetas X)
	)
	plotW := w - padL - padR
	plotH := h - padT - padB

	max := 0
	for _, p := range points {
		if p.Count > max {
			max = p.Count
		}
	}
	if max <= 0 {
		max = 1
	}
	top := float64(max) * 1.1 // aire arriba

	// Coordenadas de cada punto.
	x := func(i int) float64 {
		if len(points) == 1 {
			return float64(padL) + float64(plotW)/2
		}
		return float64(padL) + float64(plotW)*float64(i)/float64(len(points)-1)
	}
	y := func(v int) float64 {
		return float64(padT) + float64(plotH)*(1-float64(v)/top)
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %d %d" width="100%%" role="img" class="trendchart" preserveAspectRatio="xMidYMid meet">`, w, h)

	baseY := padT + plotH
	// Eje base.
	fmt.Fprintf(&b, `<line x1="%d" y1="%d" x2="%d" y2="%d" class="trend-axis"/>`, padL, baseY, w-padR, baseY)

	// Área (polígono cerrado contra la base).
	var area strings.Builder
	fmt.Fprintf(&area, "%.1f,%d ", x(0), baseY)
	for i, p := range points {
		fmt.Fprintf(&area, "%.1f,%.1f ", x(i), y(p.Count))
	}
	fmt.Fprintf(&area, "%.1f,%d", x(len(points)-1), baseY)
	fmt.Fprintf(&b, `<polygon points="%s" class="trend-area"/>`, strings.TrimSpace(area.String()))

	// Línea.
	var line strings.Builder
	for i, p := range points {
		fmt.Fprintf(&line, "%.1f,%.1f ", x(i), y(p.Count))
	}
	fmt.Fprintf(&b, `<polyline points="%s" class="trend-line"/>`, strings.TrimSpace(line.String()))

	// Adelgazar etiquetas X a ~8 como máximo.
	step := 1
	if len(points) > 8 {
		step = (len(points) + 7) / 8
	}
	for i, p := range points {
		px, py := x(i), y(p.Count)
		fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="3" class="trend-dot"><title>%s: %d</title></circle>`,
			px, py, html.EscapeString(p.Label), p.Count)
		if i%step == 0 || i == len(points)-1 {
			fmt.Fprintf(&b, `<text x="%.1f" y="%d" text-anchor="middle" class="trend-xlabel">%s</text>`,
				px, baseY+16, html.EscapeString(p.Label))
		}
	}

	// Etiquetas Y: 0 y máximo.
	fmt.Fprintf(&b, `<text x="%d" y="%.1f" text-anchor="end" class="trend-ylabel">0</text>`, padL-6, float64(baseY)+4)
	fmt.Fprintf(&b, `<text x="%d" y="%.1f" text-anchor="end" class="trend-ylabel">%d</text>`, padL-6, y(max)+4, max)

	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}
