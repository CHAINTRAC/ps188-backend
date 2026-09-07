// Command api is the PS188 backend HTTP server.
//
// Boot sequence: load config -> connect Mongo -> ensure indexes -> seed the
// bootstrap admin (first run only) -> build the router -> serve with graceful
// shutdown.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sih26/ps188-backend/internal/config"
	"github.com/sih26/ps188-backend/internal/database"
	"github.com/sih26/ps188-backend/internal/platform/jwt"
	"github.com/sih26/ps188-backend/internal/repository"
	"github.com/sih26/ps188-backend/internal/screening"
	"github.com/sih26/ps188-backend/internal/service"
	"github.com/sih26/ps188-backend/internal/storage"
	httptransport "github.com/sih26/ps188-backend/internal/transport/http"
	"github.com/sih26/ps188-backend/pkg/logger"
)

func main() {
	log := logger.New(os.Getenv("LOG_LEVEL"))
	if err := run(log); err != nil {
		log.Error("fatal", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log.Info("starting ps188 backend", slog.String("env", cfg.Env), slog.String("port", cfg.Port))

	ctx := context.Background()
	client, db, err := database.Connect(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		return err
	}
	defer func() { _ = client.Disconnect(context.Background()) }()
	log.Info("connected to mongodb", slog.String("db", cfg.MongoDB))

	if err := database.EnsureIndexes(ctx, db); err != nil {
		return err
	}

	// Repositories.
	userRepo := repository.NewUserRepository(db)
	screeningRepo := repository.NewScreeningRepository(db)
	auditRepo := repository.NewAuditRepository(db)
	blacklistRepo := repository.NewBlacklistRepository(db)
	checkpointRepo := repository.NewCheckpointRepository(db)

	// External screening engine.
	var engine screening.Engine
	switch cfg.ScreeningEngine {
	case "mock":
		engine = &screening.MockEngine{}
		log.Warn("screening engine: using MOCK implementation")
	default:
		engine = screening.NewHTTPEngine(cfg.ScreeningServiceURL, cfg.ScreeningAPIKey, cfg.ScreeningTimeout)
		log.Info("screening engine: http", slog.String("url", cfg.ScreeningServiceURL))
	}

	// Document image storage.
	var fileStore storage.FileStore
	switch cfg.StorageDriver {
	case "gridfs":
		fileStore = storage.NewGridFS(db)
		log.Info("file storage: gridfs")
	default:
		fileStore, err = storage.NewLocalFileStore(cfg.LocalStorageDir)
		if err != nil {
			return err
		}
		log.Info("file storage: local", slog.String("dir", cfg.LocalStorageDir))
	}

	// JWT + services.
	jwtMgr := jwt.NewManager(cfg.JWTSecret, cfg.JWTRefreshSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL)
	checkpointSvc := service.NewCheckpointService(checkpointRepo, auditRepo)
	authSvc := service.NewAuthService(userRepo, auditRepo, jwtMgr)
	userSvc := service.NewUserService(userRepo, auditRepo, checkpointSvc)
	blacklistSvc := service.NewBlacklistService(blacklistRepo, auditRepo)
	screeningSvc := service.NewScreeningService(screeningRepo, auditRepo, fileStore, engine, blacklistSvc, log)
	auditSvc := service.NewAuditService(auditRepo)

	if seeded, err := userSvc.SeedAdmin(ctx, cfg.SuperAdminUsername, cfg.SuperAdminPassword, cfg.SuperAdminEmail); err != nil {
		return err
	} else if seeded {
		log.Warn("seeded bootstrap super admin — change the password immediately",
			slog.String("username", cfg.SuperAdminUsername))
	}

	router := httptransport.NewRouter(httptransport.Deps{
		Cfg: cfg, Log: log, Mongo: db, JWT: jwtMgr,
		Auth: authSvc, Users: userSvc, Screenings: screeningSvc,
		Blacklist: blacklistSvc, Checkpoints: checkpointSvc, Audit: auditSvc,
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serverErr:
		return err
	case <-stop:
		log.Info("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
