package handlers

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"inventory-webapp/internal/services"
	"inventory-webapp/templates/pages"
)

func tokenErrorMessage(err error) string {
	if errors.Is(err, services.ErrTokenExpired) {
		return "El enlace ha expirado. Solicite uno nuevo."
	}
	if errors.Is(err, services.ErrTokenUsed) {
		return "Este enlace ya fue utilizado."
	}
	return "El enlace no es válido."
}

var resetPasswordErrors = map[error]string{
	services.ErrPasswordTooShort: "La contraseña debe tener al menos 6 caracteres",
	services.ErrPasswordTooLong:  "La contraseña no puede tener más de 72 caracteres",
}

type PasswordResetHandler struct {
	resetService *services.PasswordResetService
	authService  *services.AuthService
	emailService *services.EmailService
	sessions     *SessionStore
	appURL       string
}

func NewPasswordResetHandler(
	resetService *services.PasswordResetService,
	authService *services.AuthService,
	emailService *services.EmailService,
	sessions *SessionStore,
	appURL string,
) *PasswordResetHandler {
	return &PasswordResetHandler{
		resetService: resetService,
		authService:  authService,
		emailService: emailService,
		sessions:     sessions,
		appURL:       appURL,
	}
}

// RegisterRoutes registers public password reset routes.
func (h *PasswordResetHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /forgot-password", h.ShowForgotPassword)
	mux.HandleFunc("POST /forgot-password", h.HandleForgotPassword)
	mux.HandleFunc("GET /reset-password", h.ShowResetPassword)
	mux.HandleFunc("POST /reset-password", h.HandleResetPassword)
}

func (h *PasswordResetHandler) ShowForgotPassword(w http.ResponseWriter, r *http.Request) {
	render(w, r, pages.ForgotPasswordPage("", ""))
}

func (h *PasswordResetHandler) HandleForgotPassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		render(w, r, pages.ForgotPasswordPage("Error procesando formulario", ""))
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		render(w, r, pages.ForgotPasswordPage("El email es requerido", ""))
		return
	}

	// Always show success message (security: don't reveal if email exists)
	successMsg := "Si el email está registrado, recibirá un enlace para restablecer su contraseña."

	user, err := h.authService.GetUserByEmail(r.Context(), email)
	if err != nil {
		slog.Error("failed to look up user by email", "error", err)
		render(w, r, pages.ForgotPasswordPage("", successMsg))
		return
	}

	if user != nil {
		token, err := h.resetService.GenerateToken(r.Context(), user.ID)
		if err != nil {
			slog.Error("failed to generate reset token", "error", err)
			render(w, r, pages.ForgotPasswordPage("", successMsg))
			return
		}

		resetLink := fmt.Sprintf("%s/reset-password?token=%s", h.appURL, token)
		if err := h.emailService.SendPasswordResetEmail(email, user.FullName, resetLink); err != nil {
			slog.Error("failed to send reset email", "error", err, "to", email)
		} else {
			slog.Info("Password reset email sent", "to", email, "user", user.Username)
		}
	}

	render(w, r, pages.ForgotPasswordPage("", successMsg))
}

func (h *PasswordResetHandler) ShowResetPassword(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		render(w, r, pages.ResetPasswordExpiredPage("El enlace no es válido."))
		return
	}

	_, err := h.resetService.ValidateToken(r.Context(), token)
	if err != nil {
		render(w, r, pages.ResetPasswordExpiredPage(tokenErrorMessage(err)))
		return
	}

	render(w, r, pages.ResetPasswordPage(token, ""))
}

func (h *PasswordResetHandler) HandleResetPassword(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		render(w, r, pages.ResetPasswordPage("", "Error procesando formulario"))
		return
	}

	token := r.FormValue("token")
	password := r.FormValue("password")
	confirmPassword := r.FormValue("confirm_password")

	if token == "" {
		render(w, r, pages.ResetPasswordExpiredPage("El enlace no es válido."))
		return
	}

	if password == "" {
		render(w, r, pages.ResetPasswordPage(token, "La contraseña es requerida"))
		return
	}

	if password != confirmPassword {
		render(w, r, pages.ResetPasswordPage(token, "Las contraseñas no coinciden"))
		return
	}

	userID, err := h.resetService.ResetPassword(r.Context(), token, password)
	if err != nil {
		if errors.Is(err, services.ErrTokenExpired) || errors.Is(err, services.ErrTokenUsed) || errors.Is(err, services.ErrTokenInvalid) {
			render(w, r, pages.ResetPasswordExpiredPage(tokenErrorMessage(err)))
			return
		}
		errMsg := userFriendlyError(err, resetPasswordErrors)
		logServiceError("failed to reset password", err, resetPasswordErrors)
		render(w, r, pages.ResetPasswordPage(token, errMsg))
		return
	}

	// Invalidate all existing sessions for this user after password change
	h.sessions.DeleteByUserID(userID)

	slog.Info("Password reset successful via token", "user_id", userID)
	http.Redirect(w, r, "/login?success=Contrase%C3%B1a+actualizada+correctamente.+Inicie+sesi%C3%B3n.", http.StatusSeeOther)
}
