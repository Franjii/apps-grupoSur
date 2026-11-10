// Package catalog define el registro de módulos y secciones de la plataforma.
//
// La base de datos solo guarda el ACCESO (qué módulos tiene asignado cada
// usuario). Este registro define la PRESENTACIÓN: nombre, descripción, URL y
// secciones de cada módulo. Para sumar un módulo futuro (ej. Finanzas) basta
// agregar una entrada en Build.
package catalog

import "gruposur.ar/plataforma/internal/auth"

// Section es una sección dentro de un módulo (ej. Visitas dentro de Estadísticas).
type Section struct {
	Key  string
	Name string
	Desc string
	Path string // ruta de la sección, ej. "/visitas"
}

// Module es una unidad de primer nivel, asignable por usuario. Mapea a un
// subdominio en producción.
type Module struct {
	Key      string
	Name     string
	Desc     string
	BaseURL  string // URL base del módulo (subdominio), ej. https://estadisticas.gruposur.ar
	Landing  string // path de aterrizaje dentro del módulo, ej. "/estadisticas" o "/"
	Sections []Section
}

// URL devuelve el enlace a la página de aterrizaje del módulo.
func (m Module) URL() string { return m.BaseURL + m.Landing }

// Modules es el registro completo (ordenado para la UI).
type Modules []Module

// Build arma el registro a partir de la configuración (URLs según entorno).
//
// inventarioBaseURL es la URL base del módulo Inventario, que vive en una app
// aparte (inventory-webapp) en su propio subdominio.
func Build(estadisticasBaseURL, inventarioBaseURL, complianceBaseURL string, pizarraBaseURLs ...string) Modules {
	pizarraURL := "http://localhost:8081"
	if len(pizarraBaseURLs) > 0 {
		pizarraURL = pizarraBaseURLs[0]
	}
	return Modules{
		{Key: "pizarra", Name: "Pizarra", Desc: "Proyectos, pizarras y tareas compartidas con tu equipo.", BaseURL: pizarraURL, Landing: "/"},
		{
			Key:     "estadisticas",
			Name:    "Estadísticas",
			Desc:    "Reportes y métricas operativas.",
			BaseURL: estadisticasBaseURL,
			Landing: "/estadisticas",
			Sections: []Section{
				{
					Key:  "visitas",
					Name: "Visitas",
					Desc: "Dashboard de visitas realizadas por estudio, visita, procedimiento y profesional.",
					Path: "/visitas",
				},
			},
		},
		{
			Key:     "inventario",
			Name:    "Inventario",
			Desc:    "Gestión de stock, insumos y entrega/devolución de materiales.",
			BaseURL: inventarioBaseURL,
			Landing: "/",
			// Sin secciones: la UI vive en la app inventory-webapp (otro subdominio).
		},
		{
			Key:     "compliance",
			Name:    "Compliance",
			Desc:    "Seguimiento de diarios y reportes de porcentaje compliance.",
			BaseURL: complianceBaseURL,
			Landing: "/",
		},
	}
}

// ByKey devuelve el módulo con esa clave (ok=false si no existe).
func (ms Modules) ByKey(key string) (Module, bool) {
	for _, m := range ms {
		if m.Key == key {
			return m, true
		}
	}
	return Module{}, false
}

// For devuelve los módulos del registro a los que el usuario tiene acceso.
func (ms Modules) For(u *auth.User) Modules {
	if u == nil {
		return nil
	}
	var out Modules
	for _, m := range ms {
		if u.HasModule(m.Key) {
			out = append(out, m)
		}
	}
	return out
}
