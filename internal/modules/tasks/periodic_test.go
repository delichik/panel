package tasks

import (
	"context"
	"testing"
	"time"
)

func TestIntervalCollectorThrottlesAndAdvancesAfterWork(t *testing.T) {
	runs := 0
	collector := NewIntervalCollector(20*time.Millisecond, nil, func(context.Context) (CreateBatchInput, bool, error) {
		runs++
		return CreateBatchInput{Type: "periodic_test"}, true, nil
	})
	if _, shouldRun, err := collector(context.Background()); err != nil || shouldRun {
		t.Fatalf("first call should be throttled, shouldRun=%v err=%v", shouldRun, err)
	}
	time.Sleep(25 * time.Millisecond)
	if _, shouldRun, err := collector(context.Background()); err != nil || !shouldRun {
		t.Fatalf("due call should run, shouldRun=%v err=%v", shouldRun, err)
	}
	if _, shouldRun, err := collector(context.Background()); err != nil || shouldRun {
		t.Fatalf("immediate next call should be throttled, shouldRun=%v err=%v", shouldRun, err)
	}
	if runs != 1 {
		t.Fatalf("collector ran %d times, want 1", runs)
	}
}

func TestPeriodicRunnerCollectInputsFalseSkipsTaskRecord(t *testing.T) {
	svc := newTestService(t)
	def := Definition{
		Type: "periodic_skip",
		Periodic: &Periodic{
			Interval: time.Minute,
			CollectInputs: func(context.Context) (CreateBatchInput, bool, error) {
				return CreateBatchInput{}, false, nil
			},
		},
		Execute: func(TaskContext) error {
			t.Fatal("periodic task should not run when input collection skips")
			return nil
		},
	}
	svc.MustRegister(def)
	runner := NewPeriodicRunner(svc)

	runner.run(context.Background(), def)

	result, err := svc.List(context.Background(), ListFilter{Types: []string{"periodic_skip"}, IncludeInternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 {
		t.Fatalf("expected no skipped task record, got %#v", result)
	}
}

func TestPeriodicRunnerCollectInputsCreatesSingleTaskInstance(t *testing.T) {
	svc := newTestService(t)
	ran := make(chan struct{}, 1)
	def := Definition{
		Type: "periodic_run",
		Periodic: &Periodic{
			Interval: time.Minute,
			CollectInputs: func(context.Context) (CreateBatchInput, bool, error) {
				return CreateBatchInput{
					Type:        "periodic_run",
					TriggerType: "scheduler",
					Summary:     "periodic run",
					Inputs: []CreateInput{{
						Summary:    "periodic run",
						ParamsJSON: `{"serverIds":["srv_1","srv_2"]}`,
					}},
				}, true, nil
			},
		},
		Execute: func(tc TaskContext) error {
			if tc.Task.TriggerType != "scheduler" {
				t.Fatalf("expected scheduler trigger, got %q", tc.Task.TriggerType)
			}
			if tc.Task.ParamsJSON != `{"serverIds":["srv_1","srv_2"]}` {
				t.Fatalf("expected collected params, got %s", tc.Task.ParamsJSON)
			}
			ran <- struct{}{}
			return nil
		},
	}
	svc.MustRegister(def)
	runner := NewPeriodicRunner(svc)

	runner.run(context.Background(), def)

	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("expected periodic task to run")
	}
	var result ListResult
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		result, err = svc.List(context.Background(), ListFilter{Types: []string{"periodic_run"}, IncludeInternal: true})
		if err != nil {
			t.Fatal(err)
		}
		if result.Total == 1 && result.Items[0].Status == StatusCompleted {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected one completed task record, got %#v", result)
}
