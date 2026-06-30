package tasks

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestCreateRejectsUnregisteredTaskType(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.Create(context.Background(), CreateInput{Type: "not_registered"}); err == nil {
		t.Fatal("expected unregistered task type to be rejected")
	}
}

func TestManagerResourceExclusiveReturnsActiveTask(t *testing.T) {
	svc := newTestService(t)
	svc.MustRegister(Definition{Type: "exclusive_task", ConcurrencyPolicy: ConcurrencyResourceExclusive})
	manager := NewManager(svc)
	input := CreateInput{Type: "exclusive_task", ResourceType: "server", ResourceID: "srv_1"}

	first, created, err := manager.Create(context.Background(), input, Trigger{Manual: true})
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected first task to be created")
	}
	second, created, err := manager.Create(context.Background(), input, Trigger{Manual: true})
	if err != nil {
		t.Fatal(err)
	}
	if created || second.ID != first.ID {
		t.Fatalf("expected active task reuse, created=%v first=%s second=%s", created, first.ID, second.ID)
	}
}

func TestManagerTriggerPeriodicNowPassesPayloadToCollector(t *testing.T) {
	svc := newTestService(t)
	var gotPayload any
	svc.MustRegister(Definition{
		Type:              "periodic_manual",
		ConcurrencyPolicy: ConcurrencyParallelAllowed,
		Execute:           func(TaskContext) error { return nil },
		Periodic: &Periodic{
			Interval: time.Minute,
			CollectInputs: func(_ context.Context, trigger PeriodicTrigger) (CreateBatchInput, bool, error) {
				gotPayload = trigger.Payload
				return CreateBatchInput{
					Type:        "periodic_manual",
					TriggerType: trigger.Type,
					Inputs: []CreateInput{{
						Type:         "periodic_manual",
						ResourceType: "application",
						ResourceID:   trigger.TriggerResourceID,
						TriggerType:  trigger.Type,
					}},
				}, true, nil
			},
		},
	})
	payload := struct{ ApplicationIDs []string }{ApplicationIDs: []string{"app_1"}}
	task, created, err := NewManager(svc).TriggerPeriodicNow(context.Background(), "periodic_manual", PeriodicTrigger{
		Type:                "application_save",
		TriggerResourceType: "application",
		TriggerResourceID:   "app_1",
		Payload:             payload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created || task.ResourceID != "app_1" || task.TriggerType != "application_save" {
		t.Fatalf("unexpected triggered task: created=%v task=%#v", created, task)
	}
	if !reflect.DeepEqual(gotPayload, payload) {
		t.Fatalf("payload was not passed through: %#v", gotPayload)
	}
}

func TestManagerRunInvokesHooksAndCompletes(t *testing.T) {
	svc := newTestService(t)
	completed := false
	svc.MustRegister(Definition{
		Type:              "managed_success",
		ConcurrencyPolicy: ConcurrencyParallelAllowed,
		Execute: func(tc TaskContext) error {
			return tc.Advance("running", "managed run")
		},
		OnComplete: func(context.Context, Task) error {
			completed = true
			return nil
		},
	})
	task, err := svc.Create(context.Background(), CreateInput{Type: "managed_success"})
	if err != nil {
		t.Fatal(err)
	}
	if err := NewManager(svc).Run(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusCompleted || !completed {
		t.Fatalf("expected completed task and hook, got task=%#v completed=%v", got, completed)
	}
}

func TestManagerRunInvokesFailureHook(t *testing.T) {
	svc := newTestService(t)
	failed := false
	svc.MustRegister(Definition{
		Type:              "managed_failure",
		ConcurrencyPolicy: ConcurrencyParallelAllowed,
		Execute: func(TaskContext) error {
			return errors.New("boom")
		},
		OnFailure: func(_ context.Context, _ Task, cause error) error {
			failed = cause != nil
			return nil
		},
	})
	task, err := svc.Create(context.Background(), CreateInput{Type: "managed_failure"})
	if err != nil {
		t.Fatal(err)
	}
	if err := NewManager(svc).Run(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed || !failed {
		t.Fatalf("expected failed task and hook, got task=%#v failed=%v", got, failed)
	}
}

func TestManagerCreateBatchCreatesParentAndChildren(t *testing.T) {
	svc := newTestService(t)
	var mu sync.Mutex
	ran := []string{}
	svc.MustRegister(Definition{
		Type:              "batch_task",
		ConcurrencyPolicy: ConcurrencyParallelAllowed,
		Execute: func(tc TaskContext) error {
			mu.Lock()
			ran = append(ran, tc.Task.ResourceID)
			mu.Unlock()
			return nil
		},
	})
	manager := NewManager(svc)
	parent, children, created, err := manager.CreateBatch(context.Background(), CreateBatchInput{
		Type:          "batch_task",
		OperationID:   "op_batch",
		TriggerType:   "scheduler",
		Summary:       "batch",
		ExecutionMode: ExecutionModeSerial,
		Inputs: []CreateInput{
			{ResourceType: "server", ResourceID: "srv_1"},
			{ResourceType: "server", ResourceID: "srv_2"},
		},
	}, Trigger{Type: "scheduler", Periodic: true})
	if err != nil {
		t.Fatal(err)
	}
	if !created || parent.ID == "" || parent.ChildCount != 2 || len(children) != 2 {
		t.Fatalf("expected parent and two children, parent=%#v children=%#v created=%v", parent, children, created)
	}
	if children[0].ParentTaskID != parent.ID || children[0].ChildIndex != 1 || children[1].ChildIndex != 2 {
		t.Fatalf("unexpected child metadata: %#v", children)
	}
	if err := manager.Run(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	gotParent, err := svc.Get(context.Background(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotParent.Status != StatusCompleted {
		t.Fatalf("expected completed parent, got %#v", gotParent)
	}
	gotChildren, err := svc.Children(context.Background(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, child := range gotChildren {
		if child.Status != StatusCompleted {
			t.Fatalf("expected completed child, got %#v", child)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 2 || ran[0] != "srv_1" || ran[1] != "srv_2" {
		t.Fatalf("expected serial child execution, got %#v", ran)
	}
}

func TestManagerRunParentTreatsCompletedChildrenAsAlreadyDone(t *testing.T) {
	svc := newTestService(t)
	ran := 0
	svc.MustRegister(Definition{
		Type:              "batch_child_done",
		ConcurrencyPolicy: ConcurrencyParallelAllowed,
		Execute: func(TaskContext) error {
			ran++
			return nil
		},
	})
	manager := NewManager(svc)
	parent, children, created, err := manager.CreateBatch(context.Background(), CreateBatchInput{
		Type:          "batch_child_done",
		OperationID:   "op_child_done",
		ExecutionMode: ExecutionModeSerial,
		Inputs: []CreateInput{
			{ResourceType: "server", ResourceID: "srv_1"},
			{ResourceType: "server", ResourceID: "srv_2"},
		},
	}, Trigger{Type: "user"})
	if err != nil {
		t.Fatal(err)
	}
	if !created || len(children) != 2 {
		t.Fatalf("expected batch children, parent=%#v children=%#v created=%v", parent, children, created)
	}
	for _, child := range children {
		if err := svc.Complete(context.Background(), child.ID, "child already done"); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.Run(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	gotParent, err := svc.Get(context.Background(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotParent.Status != StatusCompleted {
		t.Fatalf("expected completed parent, got %#v", gotParent)
	}
	if ran != 0 {
		t.Fatalf("completed children should not run again, ran=%d", ran)
	}
}
