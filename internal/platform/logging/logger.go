package logging

import (
	"context"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	stdoutLogger *zap.Logger
	stderrLogger *zap.Logger
)

func init() {
	stdout, err := buildLogger([]string{"stdout"}, []string{"stdout"})
	if err != nil {
		panic(err)
	}
	stderr, err := buildLogger([]string{"stderr"}, []string{"stderr"})
	if err != nil {
		panic(err)
	}
	stdoutLogger = stdout
	stderrLogger = stderr
	zap.ReplaceGlobals(stdout)
}

func buildLogger(outputs, errOutputs []string) (*zap.Logger, error) {
	cfg := zap.Config{
		Level:             atomicLevel,
		Development:       false,
		Encoding:          "json",
		OutputPaths:       outputs,
		ErrorOutputPaths:  errOutputs,
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

// Logger emits structured process logs for one stream/module.
type Logger struct {
	goCtx   context.Context
	ctx     Context
	invalid bool
}

func (l *Logger) fields(extra ...zap.Field) []zap.Field {
	fields := make([]zap.Field, 0, 6+len(extra))
	eventID := l.ctx.EventID
	if eventID == "" {
		eventID = l.ctx.StreamID
	}
	fields = append(fields,
		zap.String("eventId", eventID),
		zap.String("module", l.ctx.Module),
	)
	if l.ctx.OperationID != "" {
		fields = append(fields, zap.String("operationId", l.ctx.OperationID))
	}
	if l.ctx.ExecutionID != "" {
		fields = append(fields, zap.String("executionId", l.ctx.ExecutionID))
	}
	if l.ctx.RequestID != "" {
		fields = append(fields, zap.String("requestId", l.ctx.RequestID))
	}
	fields = append(fields, extra...)
	return fields
}

func (l *Logger) log(level zapcore.Level, msg string, fields ...zap.Field) {
	if l == nil || l.invalid {
		return
	}
	if !atomicLevel.Enabled(level) {
		return
	}
	all := l.fields(fields...)
	for _, sink := range sinksFor(l.ctx.Module, level) {
		switch sink {
		case SinkDiscard:
			continue
		case SinkFile:
			writeFileLine(level, msg, l.ctx, all)
		case SinkStderr:
			stderrLogger.Check(level, msg).Write(all...)
		default:
			stdoutLogger.Check(level, msg).Write(all...)
		}
	}
	l.persistContext(level, msg)
}

func (l *Logger) persistContext(level zapcore.Level, msg string) {
	store := getContextStore()
	if store == nil {
		return
	}
	goCtx := l.goCtx
	if goCtx == nil {
		goCtx = context.Background()
	}
	summary := msg
	if len(summary) > 512 {
		summary = summary[:512]
	}
	eventID := l.ctx.EventID
	if eventID == "" {
		eventID = l.ctx.StreamID
	}
	_ = store.Upsert(goCtx, ContextRecord{
		EventID:     eventID,
		Module:      l.ctx.Module,
		Origin:      "process",
		OperationID: l.ctx.OperationID,
		ExecutionID: l.ctx.ExecutionID,
		RequestID:   l.ctx.RequestID,
		LogFile:     CurrentLogFileName(),
		HadError:    level >= zapcore.ErrorLevel,
		Level:       level.String(),
		Summary:     summary,
	})
}

func (l *Logger) Debug(msg string, fields ...zap.Field) {
	l.log(zapcore.DebugLevel, msg, fields...)
}
func (l *Logger) Info(msg string, fields ...zap.Field) {
	l.log(zapcore.InfoLevel, msg, fields...)
}
func (l *Logger) Warn(msg string, fields ...zap.Field) {
	l.log(zapcore.WarnLevel, msg, fields...)
}
func (l *Logger) Error(msg string, fields ...zap.Field) {
	l.log(zapcore.ErrorLevel, msg, fields...)
}
func (l *Logger) Fatal(msg string, fields ...zap.Field) {
	l.log(zapcore.FatalLevel, msg, fields...)
	os.Exit(1)
}

// SwapStdoutForTest replaces the stdout sink logger; restore with the returned func.
func SwapStdoutForTest(logger *zap.Logger) func() {
	prev := stdoutLogger
	stdoutLogger = logger
	return func() { stdoutLogger = prev }
}
