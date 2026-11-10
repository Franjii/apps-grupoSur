// Package handlers conecta HTTP con los servicios (auth, visitas, importación).
package handlers

import (
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"html/template"

	"gruposur.ar/plataforma/internal/auth"
	"gruposur.ar/plataforma/internal/catalog"
	"gruposur.ar/plataforma/internal/importer"
	"gruposur.ar/plataforma/internal/visits"
	"gruposur.ar/plataforma/internal/web"
)

const chartLimit = 20 // top N de barras en el gráfico

const trendDefault = "mensual" // vista temporal por defecto

// trendViews son las vistas del gráfico de tendencia (orden de presentación).
var trendViews = []trendViewDef{
	{visits.TrendDiario, "Diario"},
	{visits.TrendSemanal, "Semanal"},
	{visits.TrendMensual, "Mensual"},
	{visits.TrendHistorico, "Histórico"},
}

type trendViewDef struct{ Key, Label string }

// trendVM es el modelo de la tarjeta de tendencia para las plantillas.
type trendVM struct {
	View  string
	Views []trendViewDef
	Chart template.HTML
	OOB   bool // true => se renderiza como swap out-of-band de HTMX
}

// normalizeTrendView valida la vista pedida contra el conjunto conocido.
func normalizeTrendView(s string) string {
	for _, v := range trendViews {
		if v.Key == s {
			return s
		}
	}
	return trendDefault
}

// moduleEstadisticas es la clave RBAC del módulo que contiene la sección Visitas.
const moduleEstadisticas = "estadisticas"

type Server struct {
	DB      *sql.DB
	Rend    *web.Renderer
	Auth    *auth.Manager
	Store   *auth.Store
	Catalog catalog.Modules

	// SSOSecret es el secreto compartido que exige el endpoint de introspección
	// (/auth/introspect) para que las apps-módulo validen sesiones. Vacío => el
	// endpoint queda deshabilitado (responde 404).
	SSOSecret string
}

// Routes registra todas las rutas y devuelve el handler raíz.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.health)
	mux.Handle("GET /static/", web.StaticHandler())

	// Acceso / sesión
	mux.HandleFunc("GET /", s.Auth.OptionalUser(s.home))
	mux.HandleFunc("GET /login", s.login)
	mux.HandleFunc("GET /auth/login", s.Auth.LoginHandler)
	mux.HandleFunc("GET /auth/callback", s.Auth.CallbackHandler(s.loginWithError))
	mux.HandleFunc("GET /logout", s.Auth.LogoutHandler)

	// SSO bridge: introspección de sesiones para las apps-módulo (ej. Inventario).
	mux.HandleFunc("GET /auth/introspect", s.introspect)
	mux.HandleFunc("GET /auth/pizarra/users", s.pizarraUsers)

	// Módulo Estadísticas — página de secciones
	mux.HandleFunc("GET /estadisticas", s.Auth.RequireModule(moduleEstadisticas, s.estadisticasLanding))

	// Módulo Estadísticas — sección Visitas
	mux.HandleFunc("GET /visitas", s.Auth.RequireModule(moduleEstadisticas, s.dashboard))
	mux.HandleFunc("GET /visitas/resultados", s.Auth.RequireModule(moduleEstadisticas, s.results))
	mux.HandleFunc("GET /visitas/tendencia", s.Auth.RequireModule(moduleEstadisticas, s.trend))
	mux.HandleFunc("GET /visitas/importar", s.Auth.RequireModule(moduleEstadisticas, s.uploadPage))
	mux.HandleFunc("POST /visitas/importar", s.Auth.RequireModule(moduleEstadisticas, s.doImport))

	// Administración de usuarios
	mux.HandleFunc("GET /admin/usuarios", s.Auth.RequireAdmin(s.adminUsers))
	mux.HandleFunc("POST /admin/usuarios/crear", s.Auth.RequireAdmin(s.adminCreate))
	mux.HandleFunc("POST /admin/usuarios/modulos", s.Auth.RequireAdmin(s.adminModules))
	mux.HandleFunc("POST /admin/usuarios/estado", s.Auth.RequireAdmin(s.adminState))

	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.Ping(); err != nil {
		http.Error(w, "db down", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok"))
}

// home: hub con tarjetas de los módulos asignados (o login si no hay sesión).
func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if u == nil {
		s.login(w, r)
		return
	}
	s.Rend.Page(w, "hub", map[string]any{
		"Title": "Inicio", "User": u, "Active": "inicio",
		"Modules": s.Catalog.For(u),
	})
}

// estadisticasLanding: página del módulo Estadísticas con tarjetas de secciones.
func (s *Server) estadisticasLanding(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	m, ok := s.Catalog.ByKey(moduleEstadisticas)
	if !ok {
		http.Error(w, "módulo no encontrado", http.StatusInternalServerError)
		return
	}
	s.Rend.Page(w, "module", map[string]any{
		"Title": m.Name, "User": u, "Active": "estadisticas",
		"Module": m,
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	s.Rend.Page(w, "login", map[string]any{"Title": "Ingresar"})
}

// introspect valida un token de sesión (cookie gs_session) para las apps-módulo
// del SSO bridge. Autenticado por secreto compartido (header X-SSO-Secret).
// Responde JSON {email, name, is_admin, modules} si la sesión es válida.
func (s *Server) introspect(w http.ResponseWriter, r *http.Request) {
	// Sin secreto configurado, el endpoint no existe.
	if s.SSOSecret == "" {
		http.NotFound(w, r)
		return
	}
	got := r.Header.Get("X-SSO-Secret")
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.SSOSecret)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		token = r.Header.Get("X-Session-Token")
	}
	if token == "" {
		http.Error(w, "missing token", http.StatusBadRequest)
		return
	}
	u, err := s.Store.UserBySession(token)
	if err != nil || u == nil {
		http.Error(w, "invalid session", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{
		"email":    u.Email,
		"name":     u.Name,
		"is_admin": u.IsAdmin,
		"modules":  u.Modules,
	})
}

func (s *Server) loginWithError(w http.ResponseWriter, r *http.Request, msg string) {
	w.WriteHeader(http.StatusUnauthorized)
	s.Rend.Page(w, "login", map[string]any{"Title": "Ingresar", "Error": msg})
}

// ── Dashboard ──────────────────────────────────────────────────────────────

func (s *Server) buildResults(f visits.Filters, showAll bool) (map[string]any, error) {
	hero, err := visits.GetHero(s.DB, f)
	if err != nil {
		return nil, err
	}
	limit := chartLimit
	if showAll {
		limit = 0 // 0 => GetChart devuelve todas las barras
	}
	bars, err := visits.GetChart(s.DB, f, limit)
	if err != nil {
		return nil, err
	}
	total, err := visits.CountGroups(s.DB, f)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"Hero":    hero,
		"Chart":   visits.RenderBarChartSVG(bars),
		"Shown":   len(bars),
		"Total":   total,
		"Limit":   chartLimit,
		"ShowAll": showAll,
	}, nil
}

// buildTrend arma el modelo de la tarjeta de tendencia para la vista dada.
func (s *Server) buildTrend(f visits.Filters, view string, oob bool) (trendVM, error) {
	pts, err := visits.GetTrend(s.DB, f, view)
	if err != nil {
		return trendVM{}, err
	}
	return trendVM{
		View:  view,
		Views: trendViews,
		Chart: visits.RenderTrendSVG(pts),
		OOB:   oob,
	}, nil
}

// facetVM es un filtro multi-selección para las plantillas.
type facetVM struct {
	ID       string
	Name     string
	Label    string
	Options  []string
	Selected []string
	OOB      bool // true => se renderiza como swap out-of-band de HTMX
}

func buildFacets(o visits.Options, f visits.Filters, oob bool) []facetVM {
	out := make([]facetVM, 0, len(visits.Facets))
	for _, d := range visits.Facets {
		out = append(out, facetVM{
			ID: d.ID, Name: d.Name, Label: d.Label,
			Options: o.Values(d.Name), Selected: f.Values(d.Name), OOB: oob,
		})
	}
	return out
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	f := visits.ParseQuery(r.URL.Query())
	opts, err := visits.GetFacetedOptions(s.DB, f)
	if err != nil {
		fail(w, err)
		return
	}
	res, err := s.buildResults(f, false)
	if err != nil {
		fail(w, err)
		return
	}
	trend, err := s.buildTrend(f, trendDefault, false)
	if err != nil {
		fail(w, err)
		return
	}
	data := map[string]any{
		"Title": "Visitas", "User": u, "Active": "visitas",
		"Options": opts, "Filters": f, "Facets": buildFacets(opts, f, false),
		"Trend": trend,
	}
	for k, v := range res {
		data[k] = v
	}
	s.Rend.Page(w, "dashboard", data)
}

// trend renderiza solo la tarjeta de tendencia (cambio de vista temporal).
func (s *Server) trend(w http.ResponseWriter, r *http.Request) {
	f := visits.ParseQuery(r.URL.Query())
	view := normalizeTrendView(r.URL.Query().Get("trend"))
	vm, err := s.buildTrend(f, view, false)
	if err != nil {
		fail(w, err)
		return
	}
	s.Rend.Partial(w, "trend", vm)
}

func (s *Server) results(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := visits.ParseQuery(q)
	changed := strings.TrimSpace(q.Get("changed"))
	view := strings.TrimSpace(q.Get("view"))

	res, err := s.buildResults(f, view == "all")
	if err != nil {
		fail(w, err)
		return
	}
	// Contenido principal -> #resultados
	s.Rend.Partial(w, "results", res)

	// Si es solo un toggle del gráfico (view presente), los filtros no cambiaron:
	// no recalculamos las facetas para no pisar los buscadores de cada filtro.
	if view != "" {
		return
	}

	// Recalcular opciones de los demás filtros (faceted) y enviarlas como
	// swaps out-of-band, salvo la del filtro que el usuario está editando.
	opts, err := visits.GetFacetedOptions(s.DB, f)
	if err != nil {
		return // los resultados ya se escribieron; no rompemos la respuesta
	}
	for _, fc := range buildFacets(opts, f, true) {
		if fc.Name == changed {
			continue
		}
		s.Rend.Partial(w, "filter_select", fc)
	}

	// Refrescar la tendencia (respeta filtros) como swap out-of-band, manteniendo
	// la vista temporal vigente que el form arrastra en el campo "trend".
	if vm, err := s.buildTrend(f, normalizeTrendView(q.Get("trend")), true); err == nil {
		s.Rend.Partial(w, "trend", vm)
	}
}

// ── Importación ────────────────────────────────────────────────────────────

func (s *Server) uploadPage(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	s.Rend.Page(w, "upload", map[string]any{"Title": "Importar", "User": u, "Active": "visitas"})
}

func (s *Server) doImport(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "archivo demasiado grande o inválido", http.StatusBadRequest)
		return
	}
	file, hdr, err := r.FormFile("archivo")
	if err != nil {
		http.Error(w, "falta el archivo", http.StatusBadRequest)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "no se pudo leer el archivo", http.StatusBadRequest)
		return
	}
	res, err := importer.ImportZip(s.DB, data, hdr.Filename, u.Email)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<div class="import-result"><h3>Error en la importación</h3><p>` +
			template.HTMLEscapeString(err.Error()) + `</p></div>`))
		return
	}
	s.Rend.Partial(w, "import_result", res)
}

// ── Administración ─────────────────────────────────────────────────────────

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	users, err := s.Store.ListUsers()
	if err != nil {
		fail(w, err)
		return
	}
	mods, err := s.Store.ListModules()
	if err != nil {
		fail(w, err)
		return
	}
	s.Rend.Page(w, "admin", map[string]any{
		"Title": "Usuarios", "User": u, "Active": "admin",
		"Users": users, "Modules": mods,
	})
}

func (s *Server) adminCreate(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	email := r.FormValue("email")
	name := r.FormValue("name")
	isAdmin := r.FormValue("is_admin") == "1"
	mods := r.Form["module"]
	if err := s.Store.CreateUser(email, name, isAdmin, mods); err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/admin/usuarios", http.StatusSeeOther)
}

func (s *Server) adminModules(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	id := parseID(r.FormValue("user_id"))
	if err := s.Store.SetUserModules(id, r.Form["module"]); err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/admin/usuarios", http.StatusSeeOther)
}

func (s *Server) adminState(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	id := parseID(r.FormValue("user_id"))
	active := r.FormValue("active") == "1"
	if err := s.Store.SetUserActive(id, active); err != nil {
		fail(w, err)
		return
	}
	http.Redirect(w, r, "/admin/usuarios", http.StatusSeeOther)
}

func parseID(s string) int64 {
	id, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return id
}

// fail responde un error 500 con el mensaje del error (uso interno de handlers).
func fail(w http.ResponseWriter, err error) {
	http.Error(w, err.Error(), http.StatusInternalServerError)
}
