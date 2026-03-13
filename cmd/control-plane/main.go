package main

import (
	"log"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"v2ray-platform/internal/api"
	"v2ray-platform/internal/auth"
	"v2ray-platform/internal/config"
	"v2ray-platform/internal/ops"
	"v2ray-platform/internal/store"
	rootmigrations "v2ray-platform/migrations"
)

func main() {
	cfg := config.LoadControlPlane()
	st, err := newStore(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer st.Close()
	if err := bootstrapAdmin(cfg, st); err != nil {
		log.Fatal(err)
	}
	monitor := ops.NewMonitor(st, cfg)
	monitor.Start()
	defer monitor.Stop()
	storeMode := "postgres"
	if cfg.DatabaseURL == "" {
		storeMode = "memory"
	}
	var sessionStore auth.SessionStore
	if storeMode == "postgres" {
		sessionStore = st
	}
	svc := api.NewControlPlaneService(
		st,
		auth.NewManager(cfg.SessionSecret, cfg.PreviousSessionSecrets, cfg.SessionTTL, sessionStore),
		monitor,
		storeMode,
		cfg.ServiceName,
		cfg.RevisionName,
		cfg.AgentDownloadURL,
		cfg.AgentMD5CacheTTL,
	)
	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           api.NewRouter(cfg, svc),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("control-plane listening on %s", cfg.ListenAddr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func newStore(cfg config.ControlPlaneConfig) (store.Store, error) {
	if cfg.DatabaseURL == "" {
		slog.Info("using in-memory store")
		return store.NewMemoryStore(), nil
	}
	dbStore, err := store.NewPostgresStore(cfg.DatabaseURL, cfg.DBMaxOpenConns, cfg.DBMaxIdleConns, cfg.DBConnMaxLifetime)
	if err != nil {
		return nil, err
	}
	if err := rootmigrations.Run(dbStore.DB()); err != nil {
		_ = dbStore.Close()
		return nil, err
	}
	slog.Info("using postgres store")
	return dbStore, nil
}

func bootstrapAdmin(cfg config.ControlPlaneConfig, st store.Store) error {
	if cfg.BootstrapAdminEmail == "" || cfg.BootstrapAdminPassword == "" {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.BootstrapAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = st.EnsureAdmin(cfg.BootstrapAdminEmail, string(hash))
	return err
}
