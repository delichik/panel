package logging

import (
	"sync/atomic"

	"go.uber.org/zap/zapcore"
)

// Sink identifies where a log line is written after level filtering.
type Sink string

const (
	SinkStdout  Sink = "stdout"
	SinkStderr  Sink = "stderr"
	SinkFile    Sink = "file"
	SinkDiscard Sink = "discard"
)

// routeKey is module + level name for override lookup.
type routeKey struct {
	module string
	level  zapcore.Level
}

// Code-defined routing: module×level overrides. Unlisted pairs default to
// file + stdout mirror (container-friendly). Empty module in a key matches any
// module for that level when looking up level-only rules.
var routeOverrides = map[routeKey][]Sink{
	// Example: noisy debug for a module can be discarded by adding:
	// {module: "agent-report", level: zapcore.DebugLevel}: {SinkDiscard},
}

var defaultSinks = []Sink{SinkFile, SinkStdout}

var invalidLogCount atomic.Uint64

func invalidLogger() *Logger {
	invalidLogCount.Add(1)
	return &Logger{invalid: true}
}

// InvalidCount returns how many log calls lacked a valid stream/module.
func InvalidCount() uint64 {
	return invalidLogCount.Load()
}

func resetInvalidCount() {
	invalidLogCount.Store(0)
}

func sinksFor(module string, level zapcore.Level) []Sink {
	if sinks, ok := routeOverrides[routeKey{module: module, level: level}]; ok {
		return sinks
	}
	if sinks, ok := routeOverrides[routeKey{module: "", level: level}]; ok {
		return sinks
	}
	return defaultSinks
}
