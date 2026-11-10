package services

import (
	"fmt"
	"html"
	"log/slog"
	"mime"
	"net/smtp"
	"strings"
)

type EmailConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
}

type EmailService struct {
	config     EmailConfig
	configured bool
}

func NewEmailService(config EmailConfig) *EmailService {
	configured := config.Host != "" && config.From != ""
	if !configured {
		slog.Warn("Email service not configured", "required", "SMTP_HOST, SMTP_FROM")
	} else {
		slog.Info("email service configured", "host", config.Host, "port", config.Port, "from", config.From)
	}
	return &EmailService{config: config, configured: configured}
}

func (s *EmailService) IsConfigured() bool {
	return s.configured
}

func (s *EmailService) SendPasswordSetupEmail(to, fullName, resetLink string) error {
	if !s.configured {
		slog.Warn("email not sent (SMTP not configured)", "to", to, "type", "password_setup")
		return nil
	}
	subject := "Configuración de contraseña - Soporte GrupoSur"
	body := buildEmailHTML(
		"Bienvenido a Soporte GrupoSur",
		fullName,
		"Se ha creado una cuenta para usted. Para configurar su contraseña, haga clic en el siguiente enlace:",
		"Configurar Contraseña",
		resetLink,
		"Este enlace expira en 24 horas. Si no solicitó esta cuenta, ignore este mensaje.",
	)
	return s.sendHTML(to, subject, body)
}

func (s *EmailService) SendPasswordResetEmail(to, fullName, resetLink string) error {
	if !s.configured {
		slog.Warn("email not sent (SMTP not configured)", "to", to, "type", "password_reset")
		return nil
	}
	subject := "Recuperación de contraseña - Soporte GrupoSur"
	body := buildEmailHTML(
		"Recuperación de Contraseña",
		fullName,
		"Recibimos una solicitud para restablecer su contraseña. Haga clic en el siguiente enlace:",
		"Restablecer Contraseña",
		resetLink,
		"Este enlace expira en 24 horas. Si usted no realizó esta solicitud, ignore este mensaje.",
	)
	return s.sendHTML(to, subject, body)
}

func (s *EmailService) SendTicketNotification(to, fullName, subject, heading, bodyText, ticketURL string) error {
	if !s.configured {
		slog.Warn("email not sent (SMTP not configured)", "to", to, "type", "ticket_notification")
		return nil
	}
	body := buildEmailHTML(
		heading,
		fullName,
		bodyText,
		"Ver Tickets",
		ticketURL,
		"Este es un mensaje automático de Soporte GrupoSur.",
	)
	return s.sendHTML(to, subject, body)
}

// ExpirationAlertItem represents a single supply expiration alert for email digest.
type ExpirationAlertItem struct {
	SupplyName     string
	StudyName      string
	LotNumber      string
	ExpirationDate string
	MilestoneLabel string // "Vencido", "Vence en 1 semana", etc.
	Severity       string // "expired", "urgent", "warning", "info"
}

func (s *EmailService) SendExpirationAlertEmail(to, fullName string, items []ExpirationAlertItem) error {
	if !s.configured {
		slog.Warn("email not sent (SMTP not configured)", "to", to, "type", "expiration_alert")
		return nil
	}
	subject := "Alertas de Vencimiento de Insumos - GrupoSur"
	body := buildExpirationAlertHTML(fullName, items)
	return s.sendHTML(to, subject, body)
}

func buildExpirationAlertHTML(fullName string, items []ExpirationAlertItem) string {
	safeFullName := html.EscapeString(fullName)

	var rows strings.Builder
	for _, item := range items {
		bgColor := "#dbeafe" // info - blue
		switch item.Severity {
		case "expired":
			bgColor = "#fee2e2" // red
		case "urgent":
			bgColor = "#ffedd5" // orange
		case "warning":
			bgColor = "#fef9c3" // yellow
		}
		fmt.Fprintf(&rows,
			`<tr style="background-color: %s;">
				<td style="padding: 8px; border: 1px solid #e2e8f0;">%s</td>
				<td style="padding: 8px; border: 1px solid #e2e8f0;">%s</td>
				<td style="padding: 8px; border: 1px solid #e2e8f0;">%s</td>
				<td style="padding: 8px; border: 1px solid #e2e8f0;">%s</td>
				<td style="padding: 8px; border: 1px solid #e2e8f0;"><strong>%s</strong></td>
			</tr>`,
			bgColor,
			html.EscapeString(item.SupplyName),
			html.EscapeString(item.StudyName),
			html.EscapeString(item.LotNumber),
			html.EscapeString(item.ExpirationDate),
			html.EscapeString(item.MilestoneLabel),
		)
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #f7f8fa; padding: 40px 0; margin: 0;">
  <div style="max-width: 700px; margin: 0 auto; background: #fff; border-radius: 8px; padding: 40px; box-shadow: 0 1px 3px rgba(0,0,0,0.1);">
    <h2 style="color: #2c5282; margin-top: 0; margin-bottom: 16px;">Alertas de Vencimiento de Insumos</h2>
    <p>Hola <strong>%s</strong>,</p>
    <p>Los siguientes insumos requieren atención por proximidad a su fecha de vencimiento:</p>
    <table style="width: 100%%; border-collapse: collapse; margin: 16px 0;">
      <thead>
        <tr style="background-color: #f1f5f9;">
          <th style="padding: 8px; border: 1px solid #e2e8f0; text-align: left;">Insumo</th>
          <th style="padding: 8px; border: 1px solid #e2e8f0; text-align: left;">Estudio</th>
          <th style="padding: 8px; border: 1px solid #e2e8f0; text-align: left;">Lote</th>
          <th style="padding: 8px; border: 1px solid #e2e8f0; text-align: left;">Vencimiento</th>
          <th style="padding: 8px; border: 1px solid #e2e8f0; text-align: left;">Estado</th>
        </tr>
      </thead>
      <tbody>
        %s
      </tbody>
    </table>
    <p style="font-size: 0.85rem; color: #4a5568;">Este es un mensaje automático de Soporte GrupoSur.</p>
  </div>
</body>
</html>`, safeFullName, rows.String())
}

func (s *EmailService) sendHTML(to, subject, htmlBody string) error {
	addr := s.config.Host + ":" + s.config.Port

	var msg strings.Builder
	fmt.Fprintf(&msg, "From: %s\r\n", s.config.From)
	fmt.Fprintf(&msg, "To: %s\r\n", to)
	fmt.Fprintf(&msg, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&msg, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&msg, "Content-Type: text/html; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)

	auth := smtp.PlainAuth("", s.config.Username, s.config.Password, s.config.Host)

	if err := smtp.SendMail(addr, auth, s.config.From, []string{to}, []byte(msg.String())); err != nil {
		return fmt.Errorf("sending email to %s: %w", to, err)
	}

	slog.Info("email sent", "to", to, "subject", subject)
	return nil
}

func buildEmailHTML(heading, fullName, bodyText, buttonLabel, link, footer string) string {
	safeHeading := html.EscapeString(heading)
	safeFullName := html.EscapeString(fullName)
	safeBodyText := html.EscapeString(bodyText)
	safeButtonLabel := html.EscapeString(buttonLabel)
	safeLink := html.EscapeString(link)
	safeFooter := html.EscapeString(footer)
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #f7f8fa; padding: 40px 0; margin: 0;">
  <div style="max-width: 500px; margin: 0 auto; background: #fff; border-radius: 8px; padding: 40px; box-shadow: 0 1px 3px rgba(0,0,0,0.1);">
    <h2 style="color: #2c5282; margin-top: 0; margin-bottom: 16px;">%s</h2>
    <p>Hola <strong>%s</strong>,</p>
    <p>%s</p>
    <p style="text-align: center; margin: 24px 0;">
      <a href="%s" style="background: #2c5282; color: #fff; padding: 12px 24px; border-radius: 6px; text-decoration: none; display: inline-block;">%s</a>
    </p>
    <p style="font-size: 0.85rem; color: #4a5568;">%s</p>
  </div>
</body>
</html>`, safeHeading, safeFullName, safeBodyText, safeLink, safeButtonLabel, safeFooter)
}
