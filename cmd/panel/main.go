package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	panelbootstrap "panel/internal/bootstrap/panel"
	"panel/internal/modules/backups"
	"panel/internal/platform/config"
	"panel/internal/platform/logging"

	"go.uber.org/zap"
)

type maintenanceApplication interface {
	ListenAndServe(address string) error
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		os.Exit(runSetupCLI(os.Args[2:]))
	}
	maintenanceMode := flag.String("maintenance-mode", backups.MaintenanceModeNormal, "startup maintenance mode")
	initRestartURL := flag.String("init-restart-url", "", "local panel_init restart URL")
	initRestartToken := flag.String("init-restart-token", "", "local panel_init restart token")
	flag.Parse()
	if *initRestartURL != "" {
		_ = os.Setenv(backups.InitRestartURLEnv, *initRestartURL)
	}
	if *initRestartToken != "" {
		_ = os.Setenv(backups.InitRestartTokenEnv, *initRestartToken)
	}

	logger := logging.L()
	defer logging.Sync()

	cfg, err := config.Load(os.Getenv("PANEL_CONFIG"))
	if err != nil {
		logger.Fatal("load config failed", zap.Error(err))
	}

	var application interface {
		Handler() http.Handler
		Close() error
	}
	if *maintenanceMode == backups.MaintenanceModeRestore && backups.PendingRestoreExists(cfg.DataRoot) {
		logger.Warn("pending restore detected; starting restore mode")
		application, err = backups.NewRestoreApp(cfg)
	} else if *maintenanceMode == backups.MaintenanceModeExport && backups.PendingExportExists(cfg.DataRoot) {
		logger.Warn("pending backup export detected; starting backup export mode")
		application, err = backups.NewExportApp(cfg)
	} else {
		if *maintenanceMode != "" && *maintenanceMode != backups.MaintenanceModeNormal {
			logger.Warn("maintenance mode requested but no matching pending work exists; starting normal mode", zap.String("mode", *maintenanceMode))
		}
		application, err = panelbootstrap.New(cfg)
	}
	if err != nil {
		logger.Fatal("initialize app failed", zap.Error(err))
	}
	defer func() {
		if err := application.Close(); err != nil {
			logger.Error("close application failed", zap.Error(err))
		}
	}()

	server := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           application.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	serve := server.ListenAndServe
	if isolated, ok := application.(maintenanceApplication); ok {
		serve = func() error { return isolated.ListenAndServe(cfg.ListenAddress) }
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("panel listening", zap.String("address", cfg.ListenAddress), zap.String("url", "http://"+cfg.ListenAddress))
		errCh <- serve()
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-stop:
		logger.Info("shutdown signal received", zap.String("signal", sig.String()))
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("server failed", zap.Error(err))
		}
	}

	if _, ok := application.(maintenanceApplication); !ok {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("server shutdown failed", zap.Error(err))
		}
	}
	logger.Info("panel shutdown complete")
}
