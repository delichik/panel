package applications

import (
	"context"
	"testing"
)

func TestRuntimeMaintenancePreservesStoppedControllerAndCleanupLifecycle(t *testing.T) {
	s, _, _, closeStore := newTestService(t)
	defer closeStore()
	if s.orchestrator.Running() {
		t.Fatal("fixture unexpectedly running")
	}
	if err := s.PauseRuntimeWriters(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.editMaintenance.Enter(); ok {
		t.Fatal("edit cleanup admitted during maintenance")
	}
	select {
	case <-s.editCleanupDone:
		t.Fatal("maintenance permanently stopped edit cleanup")
	default:
	}
	if err := s.ResumeRuntimeWriters(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.orchestrator.Running() {
		t.Fatal("maintenance started an originally stopped controller")
	}
	done, ok := s.editMaintenance.Enter()
	if !ok {
		t.Fatal("edit cleanup not resumed")
	}
	done()
}
