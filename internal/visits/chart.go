package visits

import (
	"fmt"
	"html"
	"html/template"
	"strings"
)

// emptyChart es el estado vacío común de los gráficos (barras y tendencia).
const emptyChart template.HTML = `<p class="empty">No hay visitas para los filtros seleccionados.</p>`

// RenderBarChartSVG genera un gráfico de barras horizontal en SVG a partir de
// las barras (ya ordenadas desc). Sin dependencias JS.
func RenderBarChartSVG(bars []Bar) template.HTML {
	if len(bars) == 0 {
		return emptyChart
	}

	const (
		labelW = 360 // ancho reservado para etiquetas
		barH   = 22  // alto de cada barra
		gap    = 8   // separación entre barras
		padT   = 16
		padR   = 60  // espacio a la derecha para el número
		barMax = 760 // ancho máximo de barra
	)
	max := bars[0].Count
	if max <= 0 {
		max = 1
	}
	width := labelW + barMax + padR
	height := padT*2 + len(bars)*(barH+gap)

	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %d %d" width="100%%" role="img" class="barchart" preserveAspectRatio="xMinYMin meet">`, width, height)

	for i, bar := range bars {
		y := padT + i*(barH+gap)
		w := int(float64(bar.Count) / float64(max) * float64(barMax))
		if w < 1 {
			w = 1
		}
		label := truncate(bar.Label, 52)
		// etiqueta
		fmt.Fprintf(&b,
			`<text x="%d" y="%d" text-anchor="end" class="bc-label" dominant-baseline="central">%s</text>`,
			labelW-8, y+barH/2, html.EscapeString(label))
		// título accesible con etiqueta completa
		fmt.Fprintf(&b,
			`<rect x="%d" y="%d" width="%d" height="%d" rx="3" class="bc-bar"><title>%s: %d</title></rect>`,
			labelW, y, w, barH, html.EscapeString(bar.Label), bar.Count)
		// valor
		fmt.Fprintf(&b,
			`<text x="%d" y="%d" class="bc-value" dominant-baseline="central">%d</text>`,
			labelW+w+6, y+barH/2, bar.Count)
	}
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
