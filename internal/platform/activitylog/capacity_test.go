package activitylog

import "testing"

func TestCapacityAdmissionThresholds(t *testing.T) {
	for _, v := range []struct {
		free, total uint64
		state       string
	}{{MinimumFreeBytes - 1, 10 << 30, "blocked"}, {MinimumFreeBytes, 10 << 30, "warning"}, {3 << 30, 100 << 30, "warning"}, {3 << 30, 10 << 30, "ok"}} {
		if got := classifyCapacity(v.free, v.total); got.State != v.state {
			t.Fatalf("capacity %+v want %s", got, v.state)
		}
	}
}
