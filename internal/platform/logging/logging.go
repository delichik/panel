package logging

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const DefaultLevel = "info"

var atomicLevel = zap.NewAtomicLevelAt(zap.InfoLevel)

// Sync flushes buffered log entries.
func Sync() {
	_ = stdoutLogger.Sync()
	_ = stderrLogger.Sync()
	syncFileSink()
}

func Level() string {
	return atomicLevel.Level().String()
}

func NormalizeLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", DefaultLevel:
		return DefaultLevel
	case "debug":
		return "debug"
	case "warn", "warning":
		return "warn"
	case "error":
		return "error"
	default:
		return ""
	}
}

func SetLevel(value string) error {
	level, ok := zapLevel(NormalizeLevel(value))
	if !ok {
		return errors.New("invalid log level")
	}
	atomicLevel.SetLevel(level)
	return nil
}

func zapLevel(value string) (zapcore.Level, bool) {
	switch value {
	case "debug":
		return zap.DebugLevel, true
	case "info":
		return zap.InfoLevel, true
	case "warn":
		return zap.WarnLevel, true
	case "error":
		return zap.ErrorLevel, true
	default:
		return zap.InfoLevel, false
	}
}

// HTTPMiddleware creates a per-request log stream and records completion.
func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		ctx := With(r.Context(), Context{
			StreamID:  NewStreamID(),
			Module:    "http",
			RequestID: r.Header.Get("X-Request-Id"),
		})
		r = r.WithContext(ctx)
		recorder := &responseRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		fields := []zap.Field{
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Int("status", status),
			zap.Int("bytes", recorder.bytes),
			zap.Duration("duration", time.Since(startedAt)),
			zap.String("remote_addr", clientAddress(r)),
			zap.String("user_agent", r.UserAgent()),
		}
		logger := From(ctx)
		switch {
		case status >= http.StatusInternalServerError:
			logger.Error("http request completed", fields...)
		case status >= http.StatusBadRequest:
			logger.Warn("http request completed", fields...)
		default:
			logger.Debug("http request completed", fields...)
		}
	})
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func clientAddress(r *http.Request) string {
	if host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr)); err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
