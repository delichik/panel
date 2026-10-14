package activitylog

import (
	"context"
	"database/sql"
	"path/filepath"

	panelerr "panel/internal/platform/errors"
)

const MinimumFreeBytes uint64 = 1 << 30
const WarningFreeBytes uint64 = 2 << 30

type CapacityStatus struct {
	State          string `json:"state"`
	AvailableBytes uint64 `json:"availableBytes"`
	TotalBytes     uint64 `json:"totalBytes"`
}
type capacityQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func Capacity(ctx context.Context, db capacityQuery) (CapacityStatus, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA database_list`)
	if err != nil {
		return CapacityStatus{State: "unknown"}, err
	}
	path := ""
	for rows.Next() {
		var n int
		var name, file string
		if err = rows.Scan(&n, &name, &file); err != nil {
			rows.Close()
			return CapacityStatus{State: "unknown"}, err
		}
		if name == "main" {
			path = file
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return CapacityStatus{State: "unknown"}, err
	}
	if path == "" {
		return CapacityStatus{State: "unknown"}, nil
	}
	available, total, err := filesystemCapacity(filepath.Dir(path))
	if err != nil {
		return CapacityStatus{State: "unknown"}, nil
	}
	return classifyCapacity(available, total), nil
}
func classifyCapacity(available, total uint64) CapacityStatus {
	out := CapacityStatus{State: "ok", AvailableBytes: available, TotalBytes: total}
	if available < MinimumFreeBytes {
		out.State = "blocked"
	} else if available < WarningFreeBytes || (total > 0 && float64(available)/float64(total) < .1) {
		out.State = "warning"
	}
	return out
}

// Admission protects space for in-flight evidence. It is not a retention policy.
func CheckAdmission(ctx context.Context, db capacityQuery) error {
	status, err := Capacity(ctx, db)
	if err != nil {
		return err
	}
	if status.State == "blocked" {
		return panelerr.New(503, "activity_capacity_exhausted", "New changes are paused because the audit store has less than 1 GiB free; expand storage before retrying")
	}
	return nil
}
