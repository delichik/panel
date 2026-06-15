package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"panel/internal/app"
	"panel/internal/config"
	"panel/internal/logging"

	"go.uber.org/zap"
)

func main() {
	logger := logging.L()
	defer logging.Sync()

	cfg, err := config.Load(os.Getenv("PANEL_CONFIG"))
	if err != nil {
		logger.Fatal("load config failed", zap.Error(err))
	}

	application, err := app.New(cfg)
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

	errCh := make(chan error, 1)
	go func() {
		logger.Info("panel listening", zap.String("address", cfg.ListenAddress), zap.String("url", "http://"+cfg.ListenAddress))
		errCh <- server.ListenAndServe()
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("server shutdown failed", zap.Error(err))
	}
	logger.Info("panel shutdown complete")
}
