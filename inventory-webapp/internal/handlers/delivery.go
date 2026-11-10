package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"inventory-webapp/internal/models"
	"inventory-webapp/internal/services"
	"inventory-webapp/templates/pages"
)

var deliveryErrors = map[error]string{
	services.ErrItemNotFound:                "Item no encontrado",
	services.ErrItemNotAvailableForDelivery: "El item no está disponible para entrega",
	services.ErrItemNotDelivered:            "El item no ha sido entregado",
	services.ErrParticipantRequired:         "El ID de participante es obligatorio",
	services.ErrItemAlreadyFrozen:           "El item ya está congelado",
	services.ErrItemNotFrozen:               "El item no está congelado",
	services.ErrNoDeliveryBulkItems:         "No se seleccionaron ítems para la operación",
	services.ErrInvalidDeliveryOperation:    "Tipo de operación inválido",
}

type DeliveryHandler struct {
	deliveryService *services.DeliveryService
}

func NewDeliveryHandler(deliveryService *services.DeliveryService) *DeliveryHandler {
	return &DeliveryHandler{deliveryService: deliveryService}
}

func (h *DeliveryHandler) RegisterRoutes(mux *http.ServeMux, requireAuth func(http.Handler) http.Handler) {
	protect := func(hf http.HandlerFunc) http.Handler {
		return requireAuth(hf)
	}

	mux.Handle("GET /delivery", protect(h.ShowDelivery))
	mux.Handle("GET /delivery/items/{id}", protect(h.ShowItemDetail))
	mux.Handle("POST /delivery/items/{id}/deliver", protect(h.DeliverItemFromModal))
	mux.Handle("POST /delivery/items/{id}/return", protect(h.ReturnItemFromModal))
	mux.Handle("POST /delivery/bulk-operation", protect(h.BulkOperation))
}

func (h *DeliveryHandler) ShowDelivery(w http.ResponseWriter, r *http.Request) {
	user := UserFromContext(r.Context())
	filter := r.URL.Query().Get("filter")
	if filter == "" {
		filter = "all"
	}
	page := parsePage(r)

	items, itemsTotal, err := h.deliveryService.ListReturnableItems(r.Context(), filterToStatus(filter), page)
	if err != nil {
		slog.Error("failed to list returnable items", "error", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}

	log, logTotal, err := h.deliveryService.ListDeliveryLog(r.Context(), page)
	if err != nil {
		slog.Error("failed to list delivery log", "error", err)
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}

	var pag models.Pagination
	if filter == "log" {
		pag = models.NewPagination(page, logTotal)
	} else {
		pag = models.NewPagination(page, itemsTotal)
	}

	data := pages.DeliveryPageData{
		User:       user,
		Items:      items,
		Log:        log,
		Filter:     filter,
		Pagination: pag,
	}

	if r.Header.Get("HX-Request") == "true" {
		render(w, r, pages.DeliveryTabContent(data))
		return
	}

	render(w, r, pages.DeliveryPage(data))
}

func (h *DeliveryHandler) ShowItemDetail(w http.ResponseWriter, r *http.Request) {
	id := parseID(r)
	item, err := h.deliveryService.GetReturnableItemByID(r.Context(), id)
	if err != nil {
		logServiceError("failed to get returnable item", err, deliveryErrors)
		http.Error(w, "Item no encontrado", http.StatusNotFound)
		return
	}

	render(w, r, pages.DeliveryItemModal(item, ""))
}

func (h *DeliveryHandler) DeliverItemFromModal(w http.ResponseWriter, r *http.Request) {
	id := parseID(r)
	user := UserFromContext(r.Context())
	participantID := r.FormValue("participant_id")

	err := h.deliveryService.DeliverItem(r.Context(), id, participantID, user.ID)
	if err != nil {
		logServiceError("failed to deliver item", err, deliveryErrors)
		// Re-fetch item to re-render modal with error
		item, fetchErr := h.deliveryService.GetReturnableItemByID(r.Context(), id)
		if fetchErr != nil {
			http.Error(w, "Error interno", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		render(w, r, pages.DeliveryItemModal(item, userFriendlyError(err, deliveryErrors)))
		return
	}

	// Re-fetch to get updated state
	item, err := h.deliveryService.GetReturnableItemByID(r.Context(), id)
	if err != nil {
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}
	render(w, r, pages.DeliveryItemModal(item, ""))
}

func (h *DeliveryHandler) ReturnItemFromModal(w http.ResponseWriter, r *http.Request) {
	id := parseID(r)
	user := UserFromContext(r.Context())

	err := h.deliveryService.ReturnItem(r.Context(), id, user.ID)
	if err != nil {
		logServiceError("failed to return item", err, deliveryErrors)
		item, fetchErr := h.deliveryService.GetReturnableItemByID(r.Context(), id)
		if fetchErr != nil {
			http.Error(w, "Error interno", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		render(w, r, pages.DeliveryItemModal(item, userFriendlyError(err, deliveryErrors)))
		return
	}

	item, err := h.deliveryService.GetReturnableItemByID(r.Context(), id)
	if err != nil {
		http.Error(w, "Error interno", http.StatusInternalServerError)
		return
	}
	render(w, r, pages.DeliveryItemModal(item, ""))
}

func (h *DeliveryHandler) BulkOperation(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Error procesando formulario", http.StatusBadRequest)
		return
	}

	operation := r.FormValue("operation")
	itemsJSON := r.FormValue("items")

	var items []services.BulkDeliveryItem
	if err := json.Unmarshal([]byte(itemsJSON), &items); err != nil {
		slog.Error("failed to parse bulk delivery items JSON", "error", err)
		http.Redirect(w, r, "/delivery?error=datos_invalidos", http.StatusSeeOther)
		return
	}

	user := UserFromContext(r.Context())
	err := h.deliveryService.BulkFreezeUnfreeze(r.Context(), operation, items, user.ID)
	if err != nil {
		logServiceError("failed bulk delivery operation", err, deliveryErrors)
		http.Redirect(w, r, "/delivery?error=bulk_operation", http.StatusSeeOther)
		return
	}

	opLabel := "Congelación"
	if operation == models.OperationUnfreeze {
		opLabel = "Descongelación"
	}
	slog.Info("Bulk delivery operation completed", "operation", opLabel,
		"items_count", len(items), "by", user.Username)

	http.Redirect(w, r, "/delivery", http.StatusSeeOther)
}

// filterToStatus maps filter tab values to item status values.
func filterToStatus(filter string) string {
	switch filter {
	case "not_delivered":
		return "not_delivered"
	case "delivered":
		return "delivered"
	case "returned":
		return "returned"
	case "frozen":
		return "frozen"
	default:
		return "" // "all" or "log" - no status filter
	}
}
