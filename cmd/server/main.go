// Comando server: punto de entrada de la plataforma Grupo Sur.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gruposur.ar/plataforma/internal/auth"
	"gruposur.ar/plataforma/internal/catalog"
	"gruposur.ar/plataforma/internal/config"
	"gruposur.ar/plataforma/internal/db"
	"gruposur.ar/plataforma/internal/handlers"
	"gruposur.ar/plataforma/internal/web"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("config inválida: %v", err)
	}

	sdb, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("base de datos: %v", err)
	}
	defer sdb.Close()
	if err := db.Seed(sdb, cfg.AdminEmails); err != nil {
		log.Fatalf("seed: %v", err)
	}

	rend, err := web.NewRenderer()
	if err != nil {
		log.Fatalf("plantillas: %v", err)
	}

	store := auth.NewStore(sdb)

	devEmail := ""
	if cfg.DevAuthBypass {
		devEmail = cfg.DevUserEmail
		log.Printf("⚠ DEV_AUTH_BYPASS activo: sesión automática como %s", devEmail)
	}
	am := auth.NewManager(store, cfg.CookieDomain, cfg.SecureCookie, cfg.LoginURL, devEmail)
	if !cfg.DevAuthBypass {
		// Tras autenticar, aterriza en el hub de módulos (login.gruposur.ar/).
		am.ConfigureOAuth(cfg.GoogleClientID, cfg.GoogleClientSecret,
			cfg.OAuthRedirectURL, cfg.LoginURL+"/")
	}

	cat := catalog.Build(cfg.BaseURL, cfg.InventarioBaseURL, cfg.ComplianceBaseURL, cfg.PizarraBaseURL)
	srv := &handlers.Server{DB: sdb, Rend: rend, Auth: am, Store: store, Catalog: cat, SSOSecret: cfg.SSOSharedSecret}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("escuchando en %s", cfg.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("servidor: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("apagando...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
