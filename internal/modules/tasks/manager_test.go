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

func TestManagerResourceQueueCreatesAndRunsInOrder(t *testing.T) {
	svc := newTestService(t)
	var mu sync.Mutex
	ran := []string{}
	svc.MustRegister(Definition{
		Type:              "queued_task",
		ConcurrencyPolicy: ConcurrencyResourceQueue,
		Execute: func(tc TaskContext) error {
			mu.Lock()
			ran = append(ran, tc.Task.ResourceID)
			mu.Unlock()
			return nil
		},
	})
	manager := NewManager(svc)
	first, created, err := manager.Create(context.Background(), CreateInput{Type: "queued_task", ResourceType: "server", ResourceID: "srv_1"}, Trigger{Manual: true})
	if err != nil || !created {
		t.Fatalf("first create: task=%#v created=%v err=%v", first, created, err)
	}
	second, created, err := manager.Create(context.Background(), CreateInput{Type: "queued_task", ResourceType: "server", ResourceID: "srv_1"}, Trigger{Manual: true})
	if err != nil || !created {
		t.Fatalf("second create should queue: task=%#v created=%v err=%v", second, created, err)
	}
	if first.ID == second.ID {
		t.Fatalf("queued task reused existing task: first=%s second=%s", first.ID, second.ID)
	}
	gotSecond, err := svc.Get(context.Background(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotSecond.Status != StatusQueued {
		t.Fatalf("second should be queued before first runs, got %#v", gotSecond)
	}
	if err := manager.Run(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := manager.Run(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(ran, []string{"srv_1", "srv_1"}) {
		t.Fatalf("unexpected run order %#v", ran)
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

func TestManagerCreateBatchAllowsMixedChildTypes(t *testing.T) {
	svc := newTestService(t)
	ran := []string{}
	for _, taskType := range []string{"mixed_apply", "mixed_stop"} {
		taskType := taskType
		svc.MustRegister(Definition{
			Type:              taskType,
			ConcurrencyPolicy: ConcurrencyResourceQueue,
			Execute: func(tc TaskContext) error {
				ran = append(ran, tc.Task.Type+":"+tc.Task.ResourceID)
				return nil
			},
		})
	}
	svc.MustRegister(Definition{
		Type:              "mixed_batch",
		ConcurrencyPolicy: ConcurrencyResourceExclusive,
		Execute:           func(TaskContext) error { return nil },
	})
	manager := NewManager(svc)
	parent, children, created, err := manager.CreateBatch(context.Background(), CreateBatchInput{
		Type:          "mixed_batch",
		OperationID:   "op_mixed",
		ExecutionMode: ExecutionModeSerial,
		Inputs: []CreateInput{
			{Type: "mixed_apply", ResourceType: "application", ResourceID: "app_1", ServerID: "srv_1"},
			{Type: "mixed_stop", ResourceType: "application", ResourceID: "app_1", ServerID: "srv_2"},
		},
	}, Trigger{Type: "scheduler"})
	if err != nil {
		t.Fatal(err)
	}
	if !created || parent.Type != "mixed_batch" || len(children) != 2 || children[0].Type != "mixed_apply" || children[1].Type != "mixed_stop" {
		t.Fatalf("unexpected mixed batch: parent=%#v children=%#v created=%v", parent, children, created)
	}
	if err := manager.Run(context.Background(), parent); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ran, []string{"mixed_apply:app_1", "mixed_stop:app_1"}) {
		t.Fatalf("mixed batch run order = %#v", ran)
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
