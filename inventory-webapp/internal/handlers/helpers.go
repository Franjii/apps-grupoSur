package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/a-h/templ"
)

// render renders a templ component to the response writer and logs any error.
func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := c.Render(r.Context(), w); err != nil {
		slog.Error("failed to render template", "error", err)
	}
}

func parseID(r *http.Request) string {
	return r.PathValue("id")
}

// setHTMXCreateHeaders sets response headers so HTMX prepends new rows into a table body.
func setHTMXCreateHeaders(w http.ResponseWriter, target string) {
	w.Header().Set("HX-Retarget", target)
	w.Header().Set("HX-Reswap", "afterbegin")
}

// setHTMXErrorHeaders sets response headers so HTMX places the error message inside
// the dedicated action-error container above the table.
func setHTMXErrorHeaders(w http.ResponseWriter, actionErrorID string) {
	w.Header().Set("HX-Retarget", actionErrorID)
	w.Header().Set("HX-Reswap", "innerHTML")
}

// userFriendlyError maps a service error to a Spanish user-facing message.
// Returns the fallback message if the error doesn't match any known sentinel.
func userFriendlyError(err error, mappings map[error]string) string {
	for sentinel, msg := range mappings {
		if errors.Is(err, sentinel) {
			return msg
		}
	}
	return "Ha ocurrido un error inesperado"
}

// isValidationError returns true if the error is a known sentinel (validation) error.
func isValidationError(err error, mappings map[error]string) bool {
	for sentinel := range mappings {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}

// parsePage extracts the "page" query parameter, defaulting to 1.
func parsePage(r *http.Request) int {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if p < 1 {
		return 1
	}
	return p
}

// logServiceError logs at Debug for known validation errors, Error for unexpected ones.
func logServiceError(msg string, err error, mappings map[error]string, extraArgs ...any) {
	args := append([]any{"error", err}, extraArgs...)
	if isValidationError(err, mappings) {
		slog.Debug(msg, args...)
	} else {
		slog.Error(msg, args...)
	}
}
