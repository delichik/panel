package tasks

import (
	"context"
	"testing"
	"time"
)

func TestTriggerSchedulerOnceSkipsWhenCollectInputsFalse(t *testing.T) {
	svc := newTestService(t)
	def := Definition{
		Type: "periodic_skip",
		Periodic: &Periodic{
			CollectInputs: func(context.Context, PeriodicTrigger) (CreateBatchInput, bool, error) {
				return CreateBatchInput{}, false, nil
			},
		},
		Execute: func(TaskContext) error {
			t.Fatal("periodic task should not run when input collection skips")
			return nil
		},
	}
	svc.MustRegister(def)

	if _, created, err := TriggerSchedulerOnce(context.Background(), svc, def.Type); err != nil || created {
		t.Fatalf("expected skip, created=%v err=%v", created, err)
	}

	result, err := svc.List(context.Background(), ListFilter{Types: []string{"periodic_skip"}, IncludeInternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 {
		t.Fatalf("expected no skipped task record, got %#v", result)
	}
}

func TestTriggerSchedulerOnceRunsCollectorOnlyDefinition(t *testing.T) {
	svc := newTestService(t)
	collectorRuns := 0
	def := Definition{
		Type: "periodic_collector_only",
		Periodic: &Periodic{
			CollectInputs: func(context.Context, PeriodicTrigger) (CreateBatchInput, bool, error) {
				collectorRuns++
				return CreateBatchInput{}, false, nil
			},
		},
	}
	svc.MustRegister(def)

	if _, _, err := TriggerSchedulerOnce(context.Background(), svc, def.Type); err != nil {
		t.Fatal(err)
	}
	if collectorRuns != 1 {
		t.Fatalf("collector ran %d times, want 1", collectorRuns)
	}
	result, err := svc.List(context.Background(), ListFilter{Types: []string{"periodic_collector_only"}, IncludeInternal: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 0 {
		t.Fatalf("expected no collector-only task record, got %#v", result)
	}
}

func TestTriggerSchedulerOnceCreatesSingleTaskInstance(t *testing.T) {
	svc := newTestService(t)
	ran := make(chan struct{}, 1)
	def := Definition{
		Type: "periodic_run",
		Periodic: &Periodic{
			CollectInputs: func(context.Context, PeriodicTrigger) (CreateBatchInput, bool, error) {
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

	if _, created, err := TriggerSchedulerOnce(context.Background(), svc, def.Type); err != nil || !created {
		t.Fatalf("expected created task, created=%v err=%v", created, err)
	}

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
