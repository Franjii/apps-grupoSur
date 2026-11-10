package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"inventory-webapp/internal/database"
	"inventory-webapp/internal/handlers"
	"inventory-webapp/internal/services"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	db, err := database.Open("inventory.db")
	if err != nil {
		slog.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Read configuration from environment
	appURL := strings.TrimRight(getEnv("APP_URL", "http://localhost:8080"), "/")

	emailConfig := services.EmailConfig{
		Host:     getEnv("SMTP_HOST", "smtp.gmail.com"),
		Port:     getEnv("SMTP_PORT", "587"),
		Username: getEnv("SMTP_USERNAME", ""),
		Password: getEnv("SMTP_PASSWORD", ""),
		From:     getEnv("SMTP_FROM", ""),
	}

	// Initialize services
	authService := services.NewAuthService(db)
	adminService := services.NewAdminService(db)
	emailService := services.NewEmailService(emailConfig)
	resetService := services.NewPasswordResetService(db)
	stockService := services.NewStockService(db)
	deliveryService := services.NewDeliveryService(db)
	expirationAlertService := services.NewExpirationAlertService(db, emailService)

	// Initialize session store with context-aware cleanup
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	secureCookie := strings.HasPrefix(appURL, "https://")
	sessions := handlers.NewSessionStore(secureCookie)
	sessions.StartCleanup(ctx, 30*time.Minute)

	// SSO bridge: cuando está configurado, la identidad viene de la plataforma
	// (login.gruposur.ar) vía la cookie gs_session e introspección. Si falta la
	// config, la app cae al login local usuario+contraseña.
	sso := handlers.NewSSOBridge(handlers.SSOConfig{
		IntrospectURL:  getEnv("PLATFORM_INTROSPECT_URL", ""),
		SharedSecret:   getEnv("SSO_SHARED_SECRET", ""),
		LoginURL:       getEnv("PLATFORM_LOGIN_URL", ""),
		LogoutURL:      getEnv("PLATFORM_LOGOUT_URL", ""),
		RequiredModule: getEnv("SSO_REQUIRED_MODULE", "inventario"),
	}, authService)
	if sso.Enabled() {
		slog.Info("SSO bridge enabled", "introspect", getEnv("PLATFORM_INTROSPECT_URL", ""), "module", getEnv("SSO_REQUIRED_MODULE", "inventario"))
	} else {
		slog.Warn("SSO bridge disabled — usando login local usuario+contraseña")
	}

	// Start password reset token cleanup goroutine
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("Password reset token cleanup stopped")
				return
			case <-ticker.C:
				if err := resetService.CleanupExpiredTokens(ctx); err != nil {
					slog.Error("Failed to clean up expired tokens", "error", err)
				}
			}
		}
	}()

	// Start expiration alert check goroutine (daily)
	go func() {
		// Delay initial check to avoid blocking HTTP requests on the single SQLite connection
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Minute):
		}
		if err := expirationAlertService.CheckAndNotify(ctx); err != nil {
			slog.Error("Expiration alert check failed", "error", err)
		}
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				slog.Info("Expiration alert check stopped")
				return
			case <-ticker.C:
				if err := expirationAlertService.CheckAndNotify(ctx); err != nil {
					slog.Error("Expiration alert check failed", "error", err)
				}
			}
		}
	}()

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(authService, sessions, sso)
	dashboardHandler := handlers.NewDashboardHandler()
	adminHandler := handlers.NewAdminHandler(adminService)
	resetHandler := handlers.NewPasswordResetHandler(resetService, authService, emailService, sessions, appURL)
	stockHandler := handlers.NewStockHandler(stockService)
	deliveryHandler := handlers.NewDeliveryHandler(deliveryService)

	// Auth middleware
	requireAuth := handlers.RequireAuth(sessions, authService, sso)

	mux := http.NewServeMux()

	// Serve static files (no auth required) with cache headers
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.Dir("static")))
	mux.Handle("GET /static/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		staticHandler.ServeHTTP(w, r)
	}))

	// Serve service worker from root for proper PWA scope
	mux.HandleFunc("GET /sw.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, "static/sw.js")
	})

	// Public routes
	authHandler.RegisterRoutes(mux)
	resetHandler.RegisterRoutes(mux)

	// Admin routes (auth middleware: alcanza con tener el módulo inventario)
	adminHandler.RegisterRoutes(mux, requireAuth)

	// Stock routes (auth middleware)
	stockHandler.RegisterRoutes(mux, requireAuth)

	// Delivery routes (auth middleware)
	deliveryHandler.RegisterRoutes(mux, requireAuth)

	// Dashboard routes (auth middleware)
	dashboardHandler.RegisterRoutes(mux, requireAuth)

	addr := ":4040"
	csrfProtect := handlers.CSRFProtect(secureCookie)
	maxBodySize := handlers.MaxBodySize(1048576) // 1MB
	srv := &http.Server{
		Addr:         addr,
		Handler:      maxBodySize(csrfProtect(mux)),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		slog.Info("Shutting down server...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("Server shutdown error", "error", err)
		}
	}()

	slog.Info("Server starting", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
