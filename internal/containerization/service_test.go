package containerization

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestManagedLabelsOnlyAcceptsNewApplicationLabels(t *testing.T) {
	appID, instanceID, managed := managedLabels(map[string]string{
		"panel.application.managed":     "true",
		"panel.application.id":          "app-1",
		"panel.application.instance.id": "instance-1",
	})
	if !managed || appID != "app-1" || instanceID != "instance-1" {
		t.Fatalf("new labels not recognized: managed=%v app=%q instance=%q", managed, appID, instanceID)
	}

	if _, _, managed := managedLabels(map[string]string{
		"panel.application_id": "app-1",
		"panel.instance_id":    "instance-1",
	}); managed {
		t.Fatal("legacy labels must not be recognized")
	}
}

func TestExecuteSerializesOperationsPerServer(t *testing.T) {
	svc := &Service{queues: map[string]*serverQueue{}}
	var running int32
	var maxRunning int32
	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := svc.Execute(context.Background(), "server-1", func(context.Context) error {
				current := atomic.AddInt32(&running, 1)
				for {
					max := atomic.LoadInt32(&maxRunning)
					if current <= max || atomic.CompareAndSwapInt32(&maxRunning, max, current) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				atomic.AddInt32(&running, -1)
				return nil
			}); err != nil {
				t.Errorf("execute: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := atomic.LoadInt32(&maxRunning); got != 1 {
		t.Fatalf("same-server operations ran concurrently: max=%d", got)
	}
}

func TestExecuteAllowsDifferentServersToRunConcurrently(t *testing.T) {
	svc := &Service{queues: map[string]*serverQueue{}}
	entered := make(chan string, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup
	for _, serverID := range []string{"server-1", "server-2"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = svc.Execute(context.Background(), id, func(context.Context) error {
				entered <- id
				<-release
				return nil
			})
		}(serverID)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first server operation did not start")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("different server operation did not run concurrently")
	}
	close(release)
	wg.Wait()
}
