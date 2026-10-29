package models

import "time"

// Metrics 库存量表模型。列名/类型/默认值/约束与
// internal/platform/database/migrations.go 的 metrics 段 CREATE TABLE 逐一对应；
// 复合索引与 CHECK 约束无法用 orm tag 表达，见 models_test.go 与模块文档。

// MetricsSnapshot 对应 metrics_snapshots。
type MetricsSnapshot struct {
	ID               int64     `orm:"primary_key;auto_increment"`
	ServerID         string    `orm:"not_null"`
	Time             time.Time `orm:"not_null"`
	CPUUsagePercent  float64   `orm:"not_null"`
	MemoryUsedBytes  int64     `orm:"not_null"`
	MemoryTotalBytes int64     `orm:"not_null"`
	DiskUsedBytes    int64     `orm:"not_null"`
	DiskTotalBytes   int64     `orm:"not_null"`
	NetworkRXBps     float64   `orm:"not_null"`
	NetworkTXBps     float64   `orm:"not_null"`
	LoadAverage      string    `orm:"not_null;default:''"`
	Load1            float64   `orm:"not_null;default:0;column:load_1"`
	Load5            float64   `orm:"not_null;default:0;column:load_5"`
	Load15           float64   `orm:"not_null;default:0;column:load_15"`
	UptimeSeconds    int64     `orm:"not_null;default:0"`
	Hostname         string    `orm:"not_null;default:''"`
	KernelVersion    string    `orm:"not_null;default:''"`
	OSVersion        string    `orm:"not_null;default:''"`
}

func (*MetricsSnapshot) TableName() string { return "metrics_snapshots" }

// ExtraIndexDDL 返回 metrics_snapshots 表无法用 orm tag 表达的复合索引。
func (*MetricsSnapshot) ExtraIndexDDL() map[string][]string {
	return map[string][]string{
		"metrics_snapshots": {
			"CREATE INDEX IF NOT EXISTS idx_metrics_snapshots_server_time ON metrics_snapshots(server_id, time)",
		},
	}
}
