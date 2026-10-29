package logging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestFileSinkWritesJSONL(t *testing.T) {
	dir := t.TempDir()
	if err := SetLogDir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeFileSinkForTest)

	ctx := TestContext(t)
	From(ctx).Info("hello file sink", zap.String("k", "v"))
	Sync()
	closeFileSinkForTest()

	name := CurrentLogFileName()
	path := filepath.Join(dir, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read jsonl: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw[:len(raw)-1], &obj); err != nil {
		t.Fatalf("unmarshal: %v data=%q", err, raw)
	}
	for _, key := range []string{"ts", "level", "module", "eventId", "msg"} {
		if obj[key] == nil || obj[key] == "" {
			t.Fatalf("missing %s: %#v", key, obj)
		}
	}
	if obj["msg"] != "hello file sink" {
		t.Fatalf("msg = %#v", obj["msg"])
	}
	if obj["level"] != zapcore.InfoLevel.String() {
		t.Fatalf("level = %#v", obj["level"])
	}
	if obj["k"] != "v" {
		t.Fatalf("extra field k = %#v", obj["k"])
	}
}

func TestConfigureCreatesLogsDir(t *testing.T) {
	root := t.TempDir()
	if err := Configure(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeFileSinkForTest)
	info, err := os.Stat(filepath.Join(root, "logs"))
	if err != nil || !info.IsDir() {
		t.Fatalf("logs dir missing: %v", err)
	}
}
