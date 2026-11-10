// Package web agrupa el renderizado de plantillas HTML y los assets estáticos.
package web

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed templates/*.html
var tmplFS embed.FS

//go:embed static
var staticFS embed.FS

// Renderer mantiene las plantillas compiladas.
type Renderer struct {
	pages    map[string]*template.Template
	partials *template.Template
}

var funcs = template.FuncMap{
	"contains": func(list []string, v string) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	},
}

// NewRenderer compila las plantillas embebidas.
func NewRenderer() (*Renderer, error) {
	pageFiles := []string{"hub", "module", "dashboard", "upload", "login", "admin"}
	pages := make(map[string]*template.Template, len(pageFiles))
	for _, name := range pageFiles {
		t, err := template.New("layout").Funcs(funcs).ParseFS(
			tmplFS, "templates/layout.html", "templates/partials.html", "templates/"+name+".html")
		if err != nil {
			return nil, fmt.Errorf("compilar %s: %w", name, err)
		}
		pages[name] = t
	}
	partials, err := template.New("partials").Funcs(funcs).ParseFS(tmplFS, "templates/partials.html")
	if err != nil {
		return nil, fmt.Errorf("compilar partials: %w", err)
	}
	return &Renderer{pages: pages, partials: partials}, nil
}

// Page renderiza una página completa (con layout).
func (r *Renderer) Page(w http.ResponseWriter, name string, data any) {
	t, ok := r.pages[name]
	if !ok {
		http.Error(w, "plantilla no encontrada: "+name, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
	}
}

// Partial renderiza un fragmento (para swaps de HTMX).
func (r *Renderer) Partial(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := r.partials.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "render: "+err.Error(), http.StatusInternalServerError)
	}
}

// StaticHandler sirve los assets estáticos embebidos bajo /static/.
func StaticHandler() http.Handler {
	sub, _ := fs.Sub(staticFS, "static")
	return http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
}
