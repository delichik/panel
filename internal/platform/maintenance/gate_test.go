package maintenance

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPauseDrainsAndResumeAcceptsWriters(t *testing.T) {
	var gate Gate
	finish, ok := gate.Enter()
	if !ok {
		t.Fatal("initial writer rejected")
	}
	paused := make(chan struct{})
	go func() { gate.Pause(); close(paused) }()
	deadline := time.After(time.Second)
	for {
		gate.mu.Lock()
		blocked := gate.paused
		gate.mu.Unlock()
		if blocked {
			break
		}
		select {
		case <-deadline:
			t.Fatal("pause did not begin")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if _, ok := gate.Enter(); ok {
		t.Fatal("new writer admitted during pause")
	}
	select {
	case <-paused:
		t.Fatal("pause completed before active writer")
	default:
	}
	finish()
	select {
	case <-paused:
	case <-time.After(time.Second):
		t.Fatal("pause did not drain")
	}
	gate.Resume()
	finish, ok = gate.Enter()
	if !ok {
		t.Fatal("resume still blocks writers")
	}
	finish()
}

func TestPauseTimeoutCanResumeWithoutInterruptingActiveWriter(t *testing.T) {
	var gate Gate
	finish, _ := gate.Enter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := gate.PauseContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("pause ignored deadline: %v", err)
	}
	gate.Resume()
	next, ok := gate.Enter()
	if !ok {
		t.Fatal("failed drain left admission closed")
	}
	finish()
	next()
	if err := gate.PauseContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	gate.Resume()
}
