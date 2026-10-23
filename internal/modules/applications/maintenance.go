package applications

import "context"

// PauseRuntimeWriters is reversible; StopOrchestrator also permanently closes
// edit-session cleanup and is therefore reserved for service shutdown.
func (s *Service) PauseRuntimeWriters(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.maintenanceWasRunning = s.orchestrator != nil && s.orchestrator.Running()
	if err := s.editMaintenance.PauseContext(ctx); err != nil {
		return err
	}
	if s.orchestrator != nil {
		return s.orchestrator.Stop()
	}
	return nil
}

func (s *Service) ResumeRuntimeWriters(ctx context.Context) error {
	if s == nil {
		return nil
	}
	defer s.editMaintenance.Resume()
	if !s.maintenanceWasRunning {
		return nil
	}
	s.maintenanceWasRunning = false
	return s.StartOrchestrator(ctx)
}
