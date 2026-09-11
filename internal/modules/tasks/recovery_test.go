package tasks

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestWorkerRunNowConvertsPanicToFailure(t *testing.T) {
	svc := newTestService(t)
	svc.MustRegister(Definition{
		Type:              "panic_task",
		ConcurrencyPolicy: ConcurrencyParallelAllowed,
		Execute: func(TaskContext) error {
			panic("executor exploded")
		},
	})
	task, err := svc.Create(context.Background(), CreateInput{Type: "panic_task"})
	if err != nil {
		t.Fatal(err)
	}

	err = NewWorker(svc).RunNow(context.Background(), task)
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("expected converted panic error, got %v", err)
	}
	got, err := svc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed || !strings.Contains(got.Error, "executor exploded") {
		t.Fatalf("expected failed task with panic message, got %#v", got)
	}
	if svc.HasRunningExecution(task.ID) {
		t.Fatal("panic task should not retain a running execution")
	}
}

func TestCreateAndRunConvertsPanicToFailure(t *testing.T) {
	svc := newTestService(t)
	svc.MustRegister(Definition{
		Type:              "panic_create_run",
		ConcurrencyPolicy: ConcurrencyParallelAllowed,
		Execute: func(TaskContext) error {
			panic("create and run exploded")
		},
	})
	task, created, err := NewManager(svc).CreateAndRun(context.Background(), CreateInput{Type: "panic_create_run"}, Trigger{Type: "user", Manual: true})
	if err != nil || !created {
		t.Fatalf("create and run: task=%#v created=%v err=%v", task, created, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, getErr := svc.Get(context.Background(), task.ID)
		if getErr == nil && got.Status == StatusFailed {
			if !strings.Contains(got.Error, "create and run exploded") {
				t.Fatalf("unexpected failure message: %#v", got)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, _ := svc.Get(context.Background(), task.ID)
	t.Fatalf("expected panic task to fail, got %#v", got)
}

// TestRunChildrenParallelNoDeadlockAndRecoversPanics 让每个子任务同时产生
// "执行错误 + 子任务失败错误"两条错误（旧实现通道容量不足会死锁），并验证
// panic 被转换为任务失败而不是拖垮进程。
func TestRunChildrenParallelNoDeadlockAndRecoversPanics(t *testing.T) {
	svc := newTestService(t)
	svc.MustRegister(Definition{
		Type:              "panic_child",
		ConcurrencyPolicy: ConcurrencyParallelAllowed,
		Execute: func(TaskContext) error {
			panic("child exploded")
		},
	})
	manager := NewManager(svc)
	parent, _, created, err := manager.CreateBatch(context.Background(), CreateBatchInput{
		Type:          "panic_child",
		OperationID:   "op_panic",
		ExecutionMode: ExecutionModeParallel,
		Inputs: []CreateInput{
			{ResourceType: "server", ResourceID: "srv_1"},
			{ResourceType: "server", ResourceID: "srv_2"},
			{ResourceType: "server", ResourceID: "srv_3"},
		},
	}, Trigger{Type: "user"})
	if err != nil || !created {
		t.Fatalf("create batch: parent=%#v created=%v err=%v", parent, created, err)
	}
	done := make(chan error, 1)
	go func() { done <- manager.Run(context.Background(), parent) }()
	select {
	case <-done:
		// RunParent 内部已把子任务错误写入父任务 Fail，返回值可能为 nil；
		// 关键是不能死锁，且父任务必须进入 failed 终态。
	case <-time.After(5 * time.Second):
		t.Fatal("runChildrenParallel deadlocked on multiple child errors")
	}
	gotParent, err := svc.Get(context.Background(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gotParent.Status != StatusFailed {
		t.Fatalf("expected parent to fail, got %#v", gotParent)
	}
	children, err := svc.Children(context.Background(), parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, child := range children {
		if child.Status != StatusFailed {
			t.Fatalf("expected child to fail, got %#v", child)
		}
	}
}