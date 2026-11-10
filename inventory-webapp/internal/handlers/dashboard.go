package handlers

import (
	"net/http"

	"inventory-webapp/templates/pages"
)

type DashboardHandler struct{}

func NewDashboardHandler() *DashboardHandler {
	return &DashboardHandler{}
}

func (h *DashboardHandler) ShowDashboard(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	render(w, r, pages.DashboardPage(user))
}

// RegisterRoutes registers dashboard routes on the mux, protected with auth middleware.
func (h *DashboardHandler) RegisterRoutes(mux *http.ServeMux, requireAuth func(http.Handler) http.Handler) {
	protect := func(hf http.HandlerFunc) http.Handler {
		return requireAuth(hf)
	}
	mux.Handle("GET /", protect(h.ShowDashboard))
}
