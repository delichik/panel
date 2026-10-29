package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	eventID := logCtx.EventID
	if eventID == "" {
		eventID = logCtx.StreamID
	}
	obj := map[string]any{
		"ts":      time.Now().UTC().Format(time.RFC3339Nano),
		"level":   level.String(),
		"module":  logCtx.Module,
		"eventId": eventID,
		"msg":     msg,
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

func closeFileSinkLocked() {
	if fileSink != nil {
		_ = fileSink.Close()
		fileSink = nil
		fileSinkDay = ""
	}
}

func closeFileSinkForTest() {
	fileSinkMu.Lock()
	defer fileSinkMu.Unlock()
	closeFileSinkLocked()
	logDir = ""
}

// CloseFileSinkForTest releases the JSONL file handle so TempDir cleanup can
// delete the log on Windows. Safe to call when no sink is open.
func CloseFileSinkForTest() {
	closeFileSinkForTest()
}

// ClearJSONLFiles closes the active sink and removes panel-*.jsonl under the
// configured log directory. The directory setting is kept so later writes reopen
// a fresh file. Safe when logging is unconfigured.
func ClearJSONLFiles() error {
	fileSinkMu.Lock()
	dir := logDir
	closeFileSinkLocked()
	fileSinkMu.Unlock()
	return removePanelJSONL(dir)
}

// ClearJSONLDir removes panel-*.jsonl under dir. If dir is the active sink
// directory, the open file handle is closed first.
func ClearJSONLDir(dir string) error {
	dir = filepath.Clean(strings.TrimSpace(dir))
	if dir == "" || dir == "." {
		return nil
	}
	fileSinkMu.Lock()
	if logDir != "" && filepath.Clean(logDir) == dir {
		closeFileSinkLocked()
	}
	fileSinkMu.Unlock()
	return removePanelJSONL(dir)
}

func removePanelJSONL(dir string) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var first error
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "panel-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		if removeErr := os.Remove(filepath.Join(dir, name)); removeErr != nil && !os.IsNotExist(removeErr) && first == nil {
			first = removeErr
		}
	}
	return first
}

func flattenFields(obj map[string]any, fields []zap.Field) {
	enc := zapcore.NewMapObjectEncoder()
	for _, f := range fields {
		switch f.Key {
		case "streamId", "eventId", "module", "operationId", "executionId", "requestId", "ts", "level", "msg":
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
