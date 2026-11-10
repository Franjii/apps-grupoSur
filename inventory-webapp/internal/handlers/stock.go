package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"inventory-webapp/internal/models"
	"inventory-webapp/internal/services"
	"inventory-webapp/templates/pages"
)

var stockOperationErrors = map[error]string{
	services.ErrSupplyRequired:      "Debe seleccionar un insumo",
	services.ErrStudyRequired:       "Debe seleccionar un estudio",
	services.ErrQuantityPositive:    "La cantidad debe ser mayor a 0",
	services.ErrInvalidOperation:    "Tipo de operación inválido",
	services.ErrInsufficientStock:   "Stock insuficiente para esta devolución",
	services.ErrSupplyInactive:      "El insumo seleccionado está inactivo",
	services.ErrStudyInactive:       "El estudio seleccionado está inactivo",
	services.ErrSerialNumberExists:  "El número de serie ya existe",
	services.ErrSupplyStudyMismatch: "El insumo seleccionado no pertenece al estudio seleccionado",
	services.ErrNoBulkItems:         "No se seleccionaron ítems para la operación",
}

type StockHandler struct {
	stockService *services.StockService
}

func NewStockHandler(stockService *services.StockService) *StockHandler {
	return &StockHandler{stockService: stockService}
}

func (h *StockHandler) RegisterRoutes(mux *http.ServeMux, requireAuth func(http.Handler) http.Handler) {
	protect := func(hf http.HandlerFunc) http.Handler {
		return requireAuth(hf)
	}

	mux.Handle("GET /stock", protect(h.ShowStock))
	mux.Handle("POST /stock/operations", protect(h.RecordOperation))
	mux.Handle("POST /stock/bulk-operation", protect(h.BulkOperation))
}

func (h *StockHandler) ShowStock(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	filter := r.URL.Query().Get("filter")
	if filter == "" {
		filter = "stock"
	}
	page := parsePage(r)

	studies, err := h.stockService.GetActiveStudies(r.Context())
	if err != nil {
		slog.Error("failed to get active studies", "error", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}

	definitions, err := h.stockService.GetActiveSupplyDefinitions(r.Context())
	if err != nil {
		slog.Error("failed to get active supply definitions", "error", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}

	currentStock, stockTotal, err := h.stockService.GetCurrentStock(r.Context(), page)
	if err != nil {
		slog.Error("failed to get current stock", "error", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}

	operations, opsTotal, err := h.stockService.ListOperations(r.Context(), page)
	if err != nil {
		slog.Error("failed to list operations", "error", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}

	alerts, err := h.stockService.GetStockAlerts(r.Context())
	if err != nil {
		slog.Error("failed to get stock alerts", "error", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}

	var pag models.Pagination
	switch filter {
	case "operations":
		pag = models.NewPagination(page, opsTotal)
	default:
		pag = models.NewPagination(page, stockTotal)
	}

	data := pages.StockPageData{
		User:              user,
		Studies:           studies,
		SupplyDefinitions: definitions,
		CurrentStock:      currentStock,
		Operations:        operations,
		Alerts:            alerts,
		Filter:            filter,
		Pagination:        pag,
	}

	// If HTMX request, return only the table partial
	if r.Header.Get("HX-Request") == "true" {
		render(w, r, pages.StockTabContent(data))
		return
	}

	render(w, r, pages.StockPage(data))
}

func (h *StockHandler) RecordOperation(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error procesando formulario", http.StatusBadRequest)
		return
	}

	quantity, _ := strconv.Atoi(r.FormValue("quantity"))

	input := services.CreateStockOperationInput{
		StudyID:            r.FormValue("study_id"),
		SupplyDefinitionID: r.FormValue("supply_definition_id"),
		Operation:          models.OperationDelivery,
		Quantity:           quantity,
		Notes:              r.FormValue("notes"),
		SerialNumbers:      r.Form["serial_numbers"],
		ExpirationDate:     r.FormValue("expiration_date"),
	}

	user := UserFromContext(r.Context())
	err := h.stockService.RecordOperation(r.Context(), input, user.ID)
	if err != nil {
		errMsg := userFriendlyError(err, stockOperationErrors)
		logServiceError("failed to record stock operation", err, stockOperationErrors)

		// Re-render the full page with error
		studies, _ := h.stockService.GetActiveStudies(r.Context())
		definitions, _ := h.stockService.GetActiveSupplyDefinitions(r.Context())
		currentStock, stockTotal, _ := h.stockService.GetCurrentStock(r.Context(), 1)
		operations, _, _ := h.stockService.ListOperations(r.Context(), 1)
		alerts, _ := h.stockService.GetStockAlerts(r.Context())

		data := pages.StockPageData{
			User:              user,
			Studies:           studies,
			SupplyDefinitions: definitions,
			CurrentStock:      currentStock,
			Operations:        operations,
			Alerts:            alerts,
			Filter:            "stock",
			Pagination:        models.NewPagination(1, stockTotal),
			FormErr:           errMsg,
			Prefill: pages.StockFormPrefill{
				SupplyDefinitionID: r.FormValue("supply_definition_id"),
				StudyID:            r.FormValue("study_id"),
				Quantity:           r.FormValue("quantity"),
				Notes:              r.FormValue("notes"),
				SerialNumbers:      r.Form["serial_numbers"],
				ExpirationDate:     r.FormValue("expiration_date"),
			},
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		render(w, r, pages.StockPage(data))
		return
	}

	slog.Info("Stock operation recorded", "operation", input.Operation,
		"supply_definition_id", input.SupplyDefinitionID, "study_id", input.StudyID,
		"quantity", input.Quantity, "by", user.Username)

	http.Redirect(w, r, "/stock", http.StatusSeeOther)
}

func (h *StockHandler) BulkOperation(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error procesando formulario", http.StatusBadRequest)
		return
	}

	operation := r.FormValue("operation")
	itemsJSON := r.FormValue("items")

	var items []services.BulkOperationItem
	if err := json.Unmarshal([]byte(itemsJSON), &items); err != nil {
		slog.Error("failed to parse bulk items JSON", "error", err)
		http.Redirect(w, r, "/stock?error=datos_invalidos", http.StatusSeeOther)
		return
	}

	user := UserFromContext(r.Context())
	err := h.stockService.BulkOperation(r.Context(), operation, items, user.ID)
	if err != nil {
		logServiceError("failed bulk stock operation", err, stockOperationErrors)
		http.Redirect(w, r, "/stock?error=bulk_operation", http.StatusSeeOther)
		return
	}

	opLabel := models.OperationLabel(operation)
	slog.Info("Bulk stock operation completed", "operation", opLabel,
		"items_count", len(items), "by", user.Username)

	http.Redirect(w, r, "/stock", http.StatusSeeOther)
}
