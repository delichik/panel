package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	fileSinkMu  sync.Mutex
	logDir      string
	fileSinkDay string
	fileSink    *os.File
)

// Configure sets the process log directory to <dataRoot>/logs and ensures it exists.
func Configure(dataRoot string) error {
	dir := filepath.Join(dataRoot, "logs")
	return SetLogDir(dir)
}

// SetLogDir sets the JSONL file sink directory (creates it if missing).
func SetLogDir(dir string) error {
	dir = filepath.Clean(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fileSinkMu.Lock()
	defer fileSinkMu.Unlock()
	if fileSink != nil && logDir != dir {
		_ = fileSink.Close()
		fileSink = nil
		fileSinkDay = ""
	}
	logDir = dir
	return nil
}

// LogDir returns the configured JSONL directory (empty if not configured).
func LogDir() string {
	fileSinkMu.Lock()
	defer fileSinkMu.Unlock()
	return logDir
}

// CurrentLogFileName returns panel-YYYYMMDD.jsonl for UTC today.
func CurrentLogFileName() string {
	return "panel-" + time.Now().UTC().Format("20060102") + ".jsonl"
}

func writeFileLine(level zapcore.Level, msg string, logCtx Context, fields []zap.Field) {
	fileSinkMu.Lock()
	defer fileSinkMu.Unlock()
	if logDir == "" {
		return
	}
	f, err := openFileSinkLocked()
	if err != nil || f == nil {
		return
	}
	obj := map[string]any{
		"ts":       time.Now().UTC().Format(time.RFC3339Nano),
		"level":    level.String(),
		"module":   logCtx.Module,
		"streamId": logCtx.StreamID,
		"msg":      msg,
	}
	if logCtx.OperationID != "" {
		obj["operationId"] = logCtx.OperationID
	}
	if logCtx.ExecutionID != "" {
		obj["executionId"] = logCtx.ExecutionID
	}
	if logCtx.RequestID != "" {
		obj["requestId"] = logCtx.RequestID
	}
	flattenFields(obj, fields)
	line, err := json.Marshal(obj)
	if err != nil {
		return
	}
	line = append(line, '\n')
	_, _ = f.Write(line)
}

func openFileSinkLocked() (*os.File, error) {
	day := time.Now().UTC().Format("20060102")
	if fileSink != nil && fileSinkDay == day {
		return fileSink, nil
	}
	if fileSink != nil {
		_ = fileSink.Close()
		fileSink = nil
		fileSinkDay = ""
	}
	name := "panel-" + day + ".jsonl"
	path := filepath.Join(logDir, name)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	fileSink = f
	fileSinkDay = day
	return f, nil
}

func syncFileSink() {
	fileSinkMu.Lock()
	defer fileSinkMu.Unlock()
	if fileSink != nil {
		_ = fileSink.Sync()
	}
}

func closeFileSinkForTest() {
	fileSinkMu.Lock()
	defer fileSinkMu.Unlock()
	if fileSink != nil {
		_ = fileSink.Close()
		fileSink = nil
		fileSinkDay = ""
	}
	logDir = ""
}

// CloseFileSinkForTest releases the JSONL file handle so TempDir cleanup can
// delete the log on Windows. Safe to call when no sink is open.
func CloseFileSinkForTest() {
	closeFileSinkForTest()
}

func flattenFields(obj map[string]any, fields []zap.Field) {
	enc := zapcore.NewMapObjectEncoder()
	for _, f := range fields {
		switch f.Key {
		case "streamId", "module", "operationId", "executionId", "requestId", "ts", "level", "msg":
			// Already set from context / required keys; skip duplicates from Logger.fields.
			continue
		}
		f.AddTo(enc)
	}
	for k, v := range enc.Fields {
		obj[k] = normalizeJSONValue(v)
	}
}

func normalizeJSONValue(v any) any {
	switch t := v.(type) {
	case error:
		return t.Error()
	case fmt.Stringer:
		return t.String()
	case []byte:
		return string(t)
	default:
		return v
	}
}
