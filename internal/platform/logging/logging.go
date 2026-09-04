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

func init() {
	logger, err := newLogger()
	if err != nil {
		panic(err)
	}
	zap.ReplaceGlobals(logger)
}

func newLogger() (*zap.Logger, error) {
	cfg := zap.Config{
		Level:             atomicLevel,
		Development:       false,
		Encoding:          "json",
		OutputPaths:       []string{"stdout"},
		ErrorOutputPaths:  []string{"stdout"},
		DisableCaller:     false,
		DisableStacktrace: false,
		EncoderConfig: zapcore.EncoderConfig{
			TimeKey:        "ts",
			LevelKey:       "level",
			NameKey:        "logger",
			CallerKey:      "caller",
			MessageKey:     "msg",
			StacktraceKey:  "stacktrace",
			LineEnding:     zapcore.DefaultLineEnding,
			EncodeLevel:    zapcore.LowercaseLevelEncoder,
			EncodeTime:     zapcore.ISO8601TimeEncoder,
			EncodeDuration: zapcore.MillisDurationEncoder,
			EncodeCaller:   zapcore.ShortCallerEncoder,
		},
	}
	return cfg.Build(zap.AddStacktrace(zapcore.ErrorLevel))
}

func L() *zap.Logger {
	return zap.L()
}

func Sync() {
	_ = L().Sync()
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

func HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
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
		switch {
		case status >= http.StatusInternalServerError:
			L().Error("http request completed", fields...)
		case status >= http.StatusBadRequest:
			L().Warn("http request completed", fields...)
		default:
			L().Debug("http request completed", fields...)
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
