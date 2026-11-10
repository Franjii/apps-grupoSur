// Package config carga la configuración de la plataforma desde variables de
// entorno (con valores por defecto razonables para desarrollo).
package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	// Servidor
	Addr     string // dirección de escucha, ej. ":8080"
	BaseURL  string // URL pública base de la sección estadísticas
	LoginURL string // URL pública del login

	// Módulos externos
	PizarraBaseURL    string
	ComplianceBaseURL string // URL base del módulo Compliance
	InventarioBaseURL string // URL base del módulo Inventario (app aparte)

	// SSO bridge: secreto compartido con las apps-módulo que introspectan sesiones.
	SSOSharedSecret string

	// Cookies / sesión
	CookieDomain string // ej. ".gruposur.ar" (vacío en local)
	SessionKey   string // clave para firmar cookies
	SecureCookie bool   // true en producción (HTTPS)

	// Base de datos
	DBPath string

	// Google OAuth
	GoogleClientID     string
	GoogleClientSecret string
	OAuthRedirectURL   string

	// Bootstrap
	AdminEmails []string // admins sembrados al iniciar

	// Desarrollo: saltea OAuth e inicia sesión como DevUserEmail
	DevAuthBypass bool
	DevUserEmail  string
}

func Load() Config {
	c := Config{
		Addr:               env("ADDR", ":8080"),
		BaseURL:            env("BASE_URL", "http://localhost:8080"),
		LoginURL:           env("LOGIN_URL", "http://localhost:8080"),
		PizarraBaseURL:     env("PIZARRA_BASE_URL", "http://localhost:8081"),
		ComplianceBaseURL:  env("COMPLIANCE_BASE_URL", "http://localhost:8081"),
		InventarioBaseURL:  env("INVENTARIO_BASE_URL", "http://localhost:4040"),
		SSOSharedSecret:    env("SSO_SHARED_SECRET", ""),
		CookieDomain:       env("COOKIE_DOMAIN", ""),
		SessionKey:         env("SESSION_KEY", "dev-insecure-session-key-change-me"),
		SecureCookie:       envBool("SECURE_COOKIE", false),
		DBPath:             env("DB_PATH", "data/gruposur.db"),
		GoogleClientID:     env("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: env("GOOGLE_CLIENT_SECRET", ""),
		OAuthRedirectURL:   env("OAUTH_REDIRECT_URL", "http://localhost:8080/auth/callback"),
		AdminEmails:        splitList(env("ADMIN_EMAILS", "sebastian.chapartegui@gruposur.ar")),
		DevAuthBypass:      envBool("DEV_AUTH_BYPASS", false),
		DevUserEmail:       env("DEV_USER_EMAIL", "sebastian.chapartegui@gruposur.ar"),
	}
	return c
}

// Validate verifica que la configuración mínima para producción esté presente.
func (c Config) Validate() error {
	if !c.DevAuthBypass {
		if c.GoogleClientID == "" || c.GoogleClientSecret == "" {
			return fmt.Errorf("GOOGLE_CLIENT_ID y GOOGLE_CLIENT_SECRET son obligatorios (o activá DEV_AUTH_BYPASS=1)")
		}
	}
	if c.SessionKey == "" {
		return fmt.Errorf("SESSION_KEY no puede estar vacío")
	}
	return nil
}

func env(k, def string) string {
	if v, ok := os.LookupEnv(k); ok {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	v, ok := os.LookupEnv(k)
	if !ok {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, strings.ToLower(t))
		}
	}
	return out
}
