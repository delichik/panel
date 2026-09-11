package backups

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"sync"
	"time"
)

type maintenanceListener struct {
	mu     sync.Mutex
	server *http.Server
}

func (l *maintenanceListener) listenAndServeTLS(address string, handler http.Handler, tlsConfig *tls.Config) error {
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig:         tlsConfig,
	}
	l.mu.Lock()
	l.server = server
	l.mu.Unlock()
	return server.ListenAndServeTLS("", "")
}

func (l *maintenanceListener) shutdown(ctx context.Context) error {
	l.mu.Lock()
	server := l.server
	l.mu.Unlock()
	if server == nil {
		return nil
	}
	err := server.Shutdown(ctx)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (l *maintenanceListener) shutdownSoon(delay time.Duration) {
	if delay <= 0 {
		delay = 800 * time.Millisecond
	}
	go func() {
		time.Sleep(delay)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = l.shutdown(ctx)
	}()
}
