package handlers

import (
	"errors"
	"log/slog"
	"net/http"

	"inventory-webapp/internal/services"
	"inventory-webapp/templates"
	"inventory-webapp/templates/pages"
)

// Entity-specific error-to-message maps.
var studyErrors = map[error]string{
	services.ErrStudyNameExists:   "El nombre del estudio ya existe",
	services.ErrStudyNameRequired: "El nombre del estudio es requerido",
	services.ErrStudyNotFound:     "Estudio no encontrado",
	services.ErrNameTooLong:       "El nombre excede la longitud máxima permitida",
}

var supplyDefErrors = map[error]string{
	services.ErrSupplyDefinitionNameExists:   "Ya existe un insumo con ese nombre para este estudio",
	services.ErrSupplyDefinitionNameRequired: "El nombre del insumo es requerido",
	services.ErrSupplyDefinitionNotFound:     "Insumo no encontrado",
	services.ErrInvalidSupplyType:            "El tipo de insumo no es válido",
	services.ErrStudyRequired:               "Debe seleccionar un estudio",
	services.ErrNameTooLong:                  "El nombre excede la longitud máxima permitida",
}

// Deactivate/activate error maps.
var deactivateStudyErrors = map[error]string{
	services.ErrStudyNotFound: "Estudio no encontrado",
}

var deactivateSupplyDefErrors = map[error]string{
	services.ErrSupplyDefinitionNotFound: "Insumo no encontrado",
}

// Activate error maps.
var activateStudyErrors = map[error]string{
	services.ErrStudyNotFound: "Estudio no encontrado",
}

var activateSupplyDefErrors = map[error]string{
	services.ErrSupplyDefinitionNotFound: "Insumo no encontrado",
}

type AdminHandler struct {
	adminService *services.AdminService
}

func NewAdminHandler(adminService *services.AdminService) *AdminHandler {
	return &AdminHandler{adminService: adminService}
}

// RegisterRoutes registers admin routes on the mux, protected with auth middleware.
// No hay chequeo de rol: bajo SSO, RequireAuth ya exige el módulo "inventario"
// (ver SSOBridge.Authenticate), así que estas páginas quedan disponibles para
// todos los usuarios con ese módulo asignado en la plataforma.
func (h *AdminHandler) RegisterRoutes(mux *http.ServeMux, requireAuth func(http.Handler) http.Handler) {
	protect := func(hf http.HandlerFunc) http.Handler {
		return requireAuth(http.HandlerFunc(hf))
	}

	// Studies CRUD
	mux.Handle("GET /admin/studies", protect(h.ListStudies))
	mux.Handle("POST /admin/studies", protect(h.CreateStudy))
	mux.Handle("GET /admin/studies/{id}/edit", protect(h.EditStudyForm))
	mux.Handle("GET /admin/studies/{id}/display", protect(h.DisplayStudyRow))
	mux.Handle("PUT /admin/studies/{id}", protect(h.UpdateStudy))
	mux.Handle("DELETE /admin/studies/{id}", protect(h.DeactivateStudy))
	mux.Handle("PUT /admin/studies/{id}/activate", protect(h.ActivateStudy))

	// Supply Definitions CRUD
	mux.Handle("GET /admin/supplies", protect(h.ListSupplyDefinitions))
	mux.Handle("POST /admin/supplies", protect(h.CreateSupplyDefinition))
	mux.Handle("GET /admin/supplies/{id}/edit", protect(h.EditSupplyDefinitionForm))
	mux.Handle("GET /admin/supplies/{id}/display", protect(h.DisplaySupplyDefinitionRow))
	mux.Handle("PUT /admin/supplies/{id}", protect(h.UpdateSupplyDefinition))
	mux.Handle("DELETE /admin/supplies/{id}", protect(h.DeactivateSupplyDefinition))
	mux.Handle("PUT /admin/supplies/{id}/activate", protect(h.ActivateSupplyDefinition))
}

// --- Studies CRUD ---

func (h *AdminHandler) ListStudies(w http.ResponseWriter, r *http.Request) {
	currentUser := UserFromContext(r.Context())
	studies, err := h.adminService.ListStudies(r.Context())
	if err != nil {
		slog.Error("failed to list studies", "error", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}
	render(w, r, pages.AdminStudiesPage(currentUser, studies, ""))
}

func (h *AdminHandler) CreateStudy(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error procesando formulario", http.StatusBadRequest)
		return
	}
	input := services.CreateStudyInput{
		Name:      r.FormValue("name"),
		Platform1: r.FormValue("platform_1"),
		Platform2: r.FormValue("platform_2"),
		Platform3: r.FormValue("platform_3"),
	}
	study, err := h.adminService.CreateStudy(r.Context(), input)
	if err != nil {
		errMsg := userFriendlyError(err, studyErrors)
		logServiceError("failed to create study", err, studyErrors)
		prefill := pages.CreateStudyFormData{
			Name:      input.Name,
			Platform1: input.Platform1,
			Platform2: input.Platform2,
			Platform3: input.Platform3,
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		render(w, r, pages.CreateStudyForm(errMsg, prefill))
		return
	}
	currentUser := UserFromContext(r.Context())
	slog.Info("Study created", "name", study.Name, "by", currentUser.Username)
	render(w, r, pages.StudyRowOOB(study))
}

func (h *AdminHandler) EditStudyForm(w http.ResponseWriter, r *http.Request) {
	id := parseID(r)
	study, err := h.adminService.GetStudyByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, services.ErrStudyNotFound) {
			http.Error(w, "Estudio no encontrado", http.StatusNotFound)
			return
		}
		slog.Error("failed to get study for edit", "id", id, "error", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}
	render(w, r, pages.StudyEditRow(study, ""))
}

func (h *AdminHandler) DisplayStudyRow(w http.ResponseWriter, r *http.Request) {
	id := parseID(r)
	study, err := h.adminService.GetStudyByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, services.ErrStudyNotFound) {
			http.Error(w, "Estudio no encontrado", http.StatusNotFound)
			return
		}
		slog.Error("failed to get study for display", "id", id, "error", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}
	render(w, r, pages.StudyDisplayRow(study))
}

func (h *AdminHandler) UpdateStudy(w http.ResponseWriter, r *http.Request) {
	id := parseID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error procesando formulario", http.StatusBadRequest)
		return
	}
	input := services.UpdateStudyInput{
		Name:      r.FormValue("name"),
		Platform1: r.FormValue("platform_1"),
		Platform2: r.FormValue("platform_2"),
		Platform3: r.FormValue("platform_3"),
	}
	study, err := h.adminService.UpdateStudy(r.Context(), id, input)
	if err != nil {
		existing, fetchErr := h.adminService.GetStudyByID(r.Context(), id)
		if fetchErr != nil {
			http.Error(w, "Error interno", http.StatusInternalServerError)
			return
		}
		errMsg := userFriendlyError(err, studyErrors)
		logServiceError("failed to update study", err, studyErrors, "id", id)
		w.WriteHeader(http.StatusUnprocessableEntity)
		render(w, r, pages.StudyEditRow(existing, errMsg))
		return
	}
	render(w, r, pages.StudyDisplayRow(study))
}

func (h *AdminHandler) DeactivateStudy(w http.ResponseWriter, r *http.Request) {
	id := parseID(r)
	currentUser := UserFromContext(r.Context())
	study, err := h.adminService.DeactivateStudy(r.Context(), id)
	if err != nil {
		errMsg := userFriendlyError(err, deactivateStudyErrors)
		logServiceError("failed to deactivate study", err, deactivateStudyErrors, "id", id)
		setHTMXErrorHeaders(w, "#studies-action-error")
		w.WriteHeader(http.StatusUnprocessableEntity)
		render(w, r, templates.ErrorMessage(errMsg))
		return
	}
	slog.Info("Study deactivated", "name", study.Name, "by", currentUser.Username)
	render(w, r, pages.StudyDisplayRowWithClear(study))
}

func (h *AdminHandler) ActivateStudy(w http.ResponseWriter, r *http.Request) {
	id := parseID(r)
	currentUser := UserFromContext(r.Context())
	study, err := h.adminService.ActivateStudy(r.Context(), id)
	if err != nil {
		errMsg := userFriendlyError(err, activateStudyErrors)
		logServiceError("failed to activate study", err, activateStudyErrors, "id", id)
		setHTMXErrorHeaders(w, "#studies-action-error")
		w.WriteHeader(http.StatusUnprocessableEntity)
		render(w, r, templates.ErrorMessage(errMsg))
		return
	}
	slog.Info("Study activated", "name", study.Name, "by", currentUser.Username)
	render(w, r, pages.StudyDisplayRowWithClear(study))
}

// --- Supply Definitions CRUD ---

func (h *AdminHandler) ListSupplyDefinitions(w http.ResponseWriter, r *http.Request) {
	currentUser := UserFromContext(r.Context())
	defs, err := h.adminService.ListSupplyDefinitions(r.Context())
	if err != nil {
		slog.Error("failed to list supply definitions", "error", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}
	studies, err := h.adminService.ListStudies(r.Context())
	if err != nil {
		slog.Error("failed to list studies for supply form", "error", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}
	render(w, r, pages.AdminSuppliesPage(currentUser, defs, studies, ""))
}

func (h *AdminHandler) CreateSupplyDefinition(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error procesando formulario", http.StatusBadRequest)
		return
	}
	input := services.CreateSupplyDefinitionInput{
		Name:                   r.FormValue("name"),
		Type:                   r.FormValue("type"),
		StudyID:                r.FormValue("study_id"),
		HasExpirationDate:      r.FormValue("has_expiration_date") == "on",
		HasSerialNumber:        r.FormValue("has_serial_number") == "on",
		ExpirationAlertEnabled: r.FormValue("expiration_alert_enabled") == "on",
	}
	def, err := h.adminService.CreateSupplyDefinition(r.Context(), input)
	if err != nil {
		errMsg := userFriendlyError(err, supplyDefErrors)
		logServiceError("failed to create supply definition", err, supplyDefErrors)
		studies, _ := h.adminService.ListStudies(r.Context())
		prefill := pages.CreateSupplyDefFormData{
			Name:                   input.Name,
			Type:                   input.Type,
			StudyID:                input.StudyID,
			HasExpirationDate:      input.HasExpirationDate,
			HasSerialNumber:        input.HasSerialNumber,
			ExpirationAlertEnabled: input.ExpirationAlertEnabled,
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		render(w, r, pages.CreateSupplyDefForm(errMsg, prefill, studies))
		return
	}
	currentUser := UserFromContext(r.Context())
	slog.Info("Supply definition created", "name", def.Name, "by", currentUser.Username)
	render(w, r, pages.SupplyDefRowOOB(def))
}

func (h *AdminHandler) EditSupplyDefinitionForm(w http.ResponseWriter, r *http.Request) {
	id := parseID(r)
	def, err := h.adminService.GetSupplyDefinitionByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, services.ErrSupplyDefinitionNotFound) {
			http.Error(w, "Insumo no encontrado", http.StatusNotFound)
			return
		}
		slog.Error("failed to get supply definition for edit", "id", id, "error", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}
	studies, _ := h.adminService.ListStudies(r.Context())
	render(w, r, pages.SupplyDefEditRow(def, studies, ""))
}

func (h *AdminHandler) DisplaySupplyDefinitionRow(w http.ResponseWriter, r *http.Request) {
	id := parseID(r)
	def, err := h.adminService.GetSupplyDefinitionByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, services.ErrSupplyDefinitionNotFound) {
			http.Error(w, "Insumo no encontrado", http.StatusNotFound)
			return
		}
		slog.Error("failed to get supply definition for display", "id", id, "error", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}
	render(w, r, pages.SupplyDefDisplayRow(def))
}

func (h *AdminHandler) UpdateSupplyDefinition(w http.ResponseWriter, r *http.Request) {
	id := parseID(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error procesando formulario", http.StatusBadRequest)
		return
	}
	input := services.UpdateSupplyDefinitionInput{
		Name:                   r.FormValue("name"),
		Type:                   r.FormValue("type"),
		StudyID:                r.FormValue("study_id"),
		HasExpirationDate:      r.FormValue("has_expiration_date") == "on",
		HasSerialNumber:        r.FormValue("has_serial_number") == "on",
		ExpirationAlertEnabled: r.FormValue("expiration_alert_enabled") == "on",
	}
	def, err := h.adminService.UpdateSupplyDefinition(r.Context(), id, input)
	if err != nil {
		existing, fetchErr := h.adminService.GetSupplyDefinitionByID(r.Context(), id)
		if fetchErr != nil {
			http.Error(w, "Error interno", http.StatusInternalServerError)
			return
		}
		errMsg := userFriendlyError(err, supplyDefErrors)
		logServiceError("failed to update supply definition", err, supplyDefErrors, "id", id)
		studies, _ := h.adminService.ListStudies(r.Context())
		w.WriteHeader(http.StatusUnprocessableEntity)
		render(w, r, pages.SupplyDefEditRow(existing, studies, errMsg))
		return
	}
	render(w, r, pages.SupplyDefDisplayRow(def))
}

func (h *AdminHandler) DeactivateSupplyDefinition(w http.ResponseWriter, r *http.Request) {
	id := parseID(r)
	currentUser := UserFromContext(r.Context())
	def, err := h.adminService.DeactivateSupplyDefinition(r.Context(), id)
	if err != nil {
		errMsg := userFriendlyError(err, deactivateSupplyDefErrors)
		logServiceError("failed to deactivate supply definition", err, deactivateSupplyDefErrors, "id", id)
		setHTMXErrorHeaders(w, "#supplies-action-error")
		w.WriteHeader(http.StatusUnprocessableEntity)
		render(w, r, templates.ErrorMessage(errMsg))
		return
	}
	slog.Info("Supply definition deactivated", "name", def.Name, "by", currentUser.Username)
	render(w, r, pages.SupplyDefDisplayRowWithClear(def))
}

func (h *AdminHandler) ActivateSupplyDefinition(w http.ResponseWriter, r *http.Request) {
	id := parseID(r)
	currentUser := UserFromContext(r.Context())
	def, err := h.adminService.ActivateSupplyDefinition(r.Context(), id)
	if err != nil {
		errMsg := userFriendlyError(err, activateSupplyDefErrors)
		logServiceError("failed to activate supply definition", err, activateSupplyDefErrors, "id", id)
		setHTMXErrorHeaders(w, "#supplies-action-error")
		w.WriteHeader(http.StatusUnprocessableEntity)
		render(w, r, templates.ErrorMessage(errMsg))
		return
	}
	slog.Info("Supply definition activated", "name", def.Name, "by", currentUser.Username)
	render(w, r, pages.SupplyDefDisplayRowWithClear(def))
}
