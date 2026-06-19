package diagnostics

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"panel/internal/modules/tasks"
)

type DatabaseSource struct {
	Name string
	DB   *sql.DB
	Path string
}

type Service struct {
	startedAt   time.Time
	databases   []DatabaseSource
	taskRuntime TaskRuntimeProvider
}

type Snapshot struct {
	CollectedAt time.Time          `json:"collectedAt"`
	Process     ProcessStats       `json:"process"`
	Memory      MemoryStats        `json:"memory"`
	Tasks       tasks.RuntimeStats `json:"tasks"`
	Databases   []DatabaseSnapshot `json:"databases"`
}

type TaskRuntimeProvider interface {
	TaskRuntime() tasks.RuntimeStats
}

type ProcessStats struct {
	StartedAt      time.Time `json:"startedAt"`
	UptimeSeconds  int64     `json:"uptimeSeconds"`
	PID            int       `json:"pid"`
	GoVersion      string    `json:"goVersion"`
	OS             string    `json:"os"`
	Architecture   string    `json:"architecture"`
	CPUCount       int       `json:"cpuCount"`
	GoroutineCount int       `json:"goroutineCount"`
	CgoCallCount   int64     `json:"cgoCallCount"`
}

type MemoryStats struct {
	AllocBytes        uint64     `json:"allocBytes"`
	TotalAllocBytes   uint64     `json:"totalAllocBytes"`
	SysBytes          uint64     `json:"sysBytes"`
	HeapAllocBytes    uint64     `json:"heapAllocBytes"`
	HeapInUseBytes    uint64     `json:"heapInUseBytes"`
	HeapIdleBytes     uint64     `json:"heapIdleBytes"`
	HeapReleasedBytes uint64     `json:"heapReleasedBytes"`
	HeapObjects       uint64     `json:"heapObjects"`
	StackInUseBytes   uint64     `json:"stackInUseBytes"`
	StackSysBytes     uint64     `json:"stackSysBytes"`
	MSpanInUseBytes   uint64     `json:"mspanInUseBytes"`
	MCacheInUseBytes  uint64     `json:"mcacheInUseBytes"`
	NextGCBytes       uint64     `json:"nextGcBytes"`
	GCCycles          uint32     `json:"gcCycles"`
	ForcedGCCycles    uint32     `json:"forcedGcCycles"`
	GCPauseTotalNs    uint64     `json:"gcPauseTotalNs"`
	LastGCAt          *time.Time `json:"lastGcAt"`
}

type DatabaseSnapshot struct {
	Name          string          `json:"name"`
	Healthy       bool            `json:"healthy"`
	ErrorCode     string          `json:"errorCode,omitempty"`
	FileSizeBytes int64           `json:"fileSizeBytes"`
	PageSizeBytes int64           `json:"pageSizeBytes"`
	PageCount     int64           `json:"pageCount"`
	FreePageCount int64           `json:"freePageCount"`
	UsedBytes     int64           `json:"usedBytes"`
	FreeBytes     int64           `json:"freeBytes"`
	Connections   ConnectionStats `json:"connections"`
	Tables        []TableStats    `json:"tables"`
}

type ConnectionStats struct {
	MaxOpenConnections int   `json:"maxOpenConnections"`
	OpenConnections    int   `json:"openConnections"`
	InUse              int   `json:"inUse"`
	Idle               int   `json:"idle"`
	WaitCount          int64 `json:"waitCount"`
	WaitDurationNs     int64 `json:"waitDurationNs"`
	MaxIdleClosed      int64 `json:"maxIdleClosed"`
	MaxIdleTimeClosed  int64 `json:"maxIdleTimeClosed"`
	MaxLifetimeClosed  int64 `json:"maxLifetimeClosed"`
}

type TableStats struct {
	Name      string `json:"name"`
	RowCount  int64  `json:"rowCount"`
	ErrorCode string `json:"errorCode,omitempty"`
}

func NewService(databases ...DatabaseSource) *Service {
	return &Service{startedAt: time.Now().UTC(), databases: databases}
}

func NewServiceWithTaskRuntime(taskRuntime TaskRuntimeProvider, databases ...DatabaseSource) *Service {
	return &Service{startedAt: time.Now().UTC(), databases: databases, taskRuntime: taskRuntime}
}

func (s *Service) Snapshot(ctx context.Context) Snapshot {
	now := time.Now().UTC()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)

	var lastGC *time.Time
	if memory.LastGC > 0 {
		value := time.Unix(0, int64(memory.LastGC)).UTC()
		lastGC = &value
	}

	out := Snapshot{
		CollectedAt: now,
		Process: ProcessStats{
			StartedAt:      s.startedAt,
			UptimeSeconds:  max(0, int64(now.Sub(s.startedAt).Seconds())),
			PID:            os.Getpid(),
			GoVersion:      runtime.Version(),
			OS:             runtime.GOOS,
			Architecture:   runtime.GOARCH,
			CPUCount:       runtime.NumCPU(),
			GoroutineCount: runtime.NumGoroutine(),
			CgoCallCount:   runtime.NumCgoCall(),
		},
		Memory: MemoryStats{
			AllocBytes:        memory.Alloc,
			TotalAllocBytes:   memory.TotalAlloc,
			SysBytes:          memory.Sys,
			HeapAllocBytes:    memory.HeapAlloc,
			HeapInUseBytes:    memory.HeapInuse,
			HeapIdleBytes:     memory.HeapIdle,
			HeapReleasedBytes: memory.HeapReleased,
			HeapObjects:       memory.HeapObjects,
			StackInUseBytes:   memory.StackInuse,
			StackSysBytes:     memory.StackSys,
			MSpanInUseBytes:   memory.MSpanInuse,
			MCacheInUseBytes:  memory.MCacheInuse,
			NextGCBytes:       memory.NextGC,
			GCCycles:          memory.NumGC,
			ForcedGCCycles:    memory.NumForcedGC,
			GCPauseTotalNs:    memory.PauseTotalNs,
			LastGCAt:          lastGC,
		},
		Databases: make([]DatabaseSnapshot, 0, len(s.databases)),
	}
	if s.taskRuntime != nil {
		out.Tasks = s.taskRuntime.TaskRuntime()
	}
	for _, source := range s.databases {
		out.Databases = append(out.Databases, collectDatabase(ctx, source))
	}
	return out
}

func collectDatabase(ctx context.Context, source DatabaseSource) DatabaseSnapshot {
	out := DatabaseSnapshot{Name: source.Name, Tables: []TableStats{}}
	if source.DB == nil {
		out.ErrorCode = "database_unavailable"
		return out
	}

	stats := source.DB.Stats()
	out.Connections = ConnectionStats{
		MaxOpenConnections: stats.MaxOpenConnections,
		OpenConnections:    stats.OpenConnections,
		InUse:              stats.InUse,
		Idle:               stats.Idle,
		WaitCount:          stats.WaitCount,
		WaitDurationNs:     stats.WaitDuration.Nanoseconds(),
		MaxIdleClosed:      stats.MaxIdleClosed,
		MaxIdleTimeClosed:  stats.MaxIdleTimeClosed,
		MaxLifetimeClosed:  stats.MaxLifetimeClosed,
	}

	if size, err := databaseFileSize(source.Path); err == nil {
		out.FileSizeBytes = size
	}
	if err := source.DB.PingContext(ctx); err != nil {
		out.ErrorCode = "database_unavailable"
		return out
	}

	if err := source.DB.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&out.PageSizeBytes); err != nil {
		out.ErrorCode = "database_stats_unavailable"
		return out
	}
	if err := source.DB.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&out.PageCount); err != nil {
		out.ErrorCode = "database_stats_unavailable"
		return out
	}
	if err := source.DB.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&out.FreePageCount); err != nil {
		out.ErrorCode = "database_stats_unavailable"
		return out
	}
	out.FreeBytes = out.PageSizeBytes * out.FreePageCount
	out.UsedBytes = out.PageSizeBytes * (out.PageCount - out.FreePageCount)

	rows, err := source.DB.QueryContext(ctx, `
		SELECT name
		FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name`)
	if err != nil {
		out.ErrorCode = "database_tables_unavailable"
		return out
	}
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			out.ErrorCode = "database_tables_unavailable"
			return out
		}
		names = append(names, name)
	}
	if err := rows.Close(); err != nil || rows.Err() != nil {
		out.ErrorCode = "database_tables_unavailable"
		return out
	}
	sort.Strings(names)
	for _, name := range names {
		table := TableStats{Name: name}
		query := `SELECT COUNT(*) FROM ` + quoteIdentifier(name)
		if err := source.DB.QueryRowContext(ctx, query).Scan(&table.RowCount); err != nil {
			table.ErrorCode = "table_count_unavailable"
		}
		out.Tables = append(out.Tables, table)
	}
	out.Healthy = out.ErrorCode == ""
	return out
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func databaseFileSize(value string) (int64, error) {
	path, err := sqliteFilePath(value)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func sqliteFilePath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("database path is empty")
	}
	if !strings.HasPrefix(filepath.ToSlash(value), "file:") {
		return value, nil
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	path := parsed.Path
	if path == "" {
		path = parsed.Opaque
	}
	if path == "" {
		return "", fmt.Errorf("database file path is unavailable")
	}
	if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	return filepath.FromSlash(path), nil
}
