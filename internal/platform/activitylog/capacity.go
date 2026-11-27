package activitylog

import (
	"context"
	"database/sql"
	"path/filepath"

	"panel/internal/platform/logging"
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
	if dir := logging.LogDir(); dir != "" {
		available, total, err := filesystemCapacity(dir)
		if err != nil {
			return CapacityStatus{State: "unknown"}, nil
		}
		return classifyCapacity(available, total), nil
	}
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

// CheckAdmission no longer blocks mutations on activity_events capacity.
// Product facts go to the JSONL log directory; capacity is advisory via Capacity.
func CheckAdmission(context.Context, capacityQuery) error {
	return nil
}
