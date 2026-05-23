package containerops

import (
	"context"

	"panel/internal/tasks"
)

type Worker struct {
	tasks *tasks.Service
	locks *LeaseService
}

func NewWorker(taskSvc *tasks.Service, locks *LeaseService) *Worker {
	return &Worker{tasks: taskSvc, locks: locks}
}

func (w *Worker) RunNow(ctx context.Context, task tasks.Task) error {
	_ = ctx
	_ = task
	return nil
}
