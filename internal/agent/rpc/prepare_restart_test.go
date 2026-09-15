package rpc

import (
	"context"
	"errors"
	"testing"
	"time"

	agentpb "panel/internal/agent/pb"

	"google.golang.org/grpc/metadata"
)

func TestPackageUpgradeTrackerIdle(t *testing.T) {
	var tracker packageUpgradeTracker
	done, active := tracker.wait()
	if active {
		t.Fatal("expected tracker to be idle")
	}
	if done != nil {
		t.Fatalf("expected nil done channel when idle, got %v", done)
	}
}

func TestPackageUpgradeTrackerLifecycle(t *testing.T) {
	var tracker packageUpgradeTracker
	tracker.begin()
	done, active := tracker.wait()
	if !active {
		t.Fatal("expected tracker to be active after begin")
	}
	tracker.begin() // concurrent upgrade
	tracker.end()
	select {
	case <-done:
		t.Fatal("done closed while another upgrade is still active")
	default:
	}
	tracker.end()
	select {
	case <-done:
	default:
		t.Fatal("done not closed after the last upgrade finished")
	}
	if _, active := tracker.wait(); active {
		t.Fatal("expected tracker to be idle after all upgrades finished")
	}
}

type prepareRestartStreamStub struct {
	ctx     context.Context
	states  []string
	sendErr error
}

func (s *prepareRestartStreamStub) SetHeader(metadata.MD) error  { return nil }
func (s *prepareRestartStreamStub) SendHeader(metadata.MD) error { return nil }
func (s *prepareRestartStreamStub) SetTrailer(metadata.MD)       {}
func (s *prepareRestartStreamStub) Context() context.Context     { return s.ctx }
func (s *prepareRestartStreamStub) SendMsg(any) error            { return nil }
func (s *prepareRestartStreamStub) RecvMsg(any) error            { return nil }
func (s *prepareRestartStreamStub) Send(resp *agentpb.PrepareRestartResponse) error {
	s.states = append(s.states, resp.GetState())
	return s.sendErr
}

func TestPrepareRestartReadyWhenIdle(t *testing.T) {
	handler := &Handler{}
	stream := &prepareRestartStreamStub{ctx: context.Background()}
	if err := handler.PrepareRestart(&agentpb.Empty{}, stream); err != nil {
		t.Fatalf("PrepareRestart returned error: %v", err)
	}
	if len(stream.states) != 1 || stream.states[0] != "ready" {
		t.Fatalf("expected single ready state, got %#v", stream.states)
	}
}

func TestPrepareRestartHoldOnUntilUpgradeFinishes(t *testing.T) {
	handler := &Handler{}
	handler.upgrades.begin()
	defer handler.upgrades.end()
	stream := &prepareRestartStreamStub{ctx: context.Background()}
	done := make(chan error, 1)
	go func() {
		done <- handler.PrepareRestart(&agentpb.Empty{}, stream)
	}()
	time.Sleep(1300 * time.Millisecond)
	if len(stream.states) == 0 {
		t.Fatal("expected at least one holdon while upgrade is active")
	}
	for _, state := range stream.states {
		if state != "holdon" {
			t.Fatalf("expected only holdon while active, got %q", state)
		}
	}
	handler.upgrades.end()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("PrepareRestart returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("PrepareRestart did not return after upgrade finished")
	}
	if len(stream.states) == 0 || stream.states[len(stream.states)-1] != "ready" {
		t.Fatalf("expected final ready state, got %#v", stream.states)
	}
}

func TestPrepareRestartStopsWhenContextCancelled(t *testing.T) {
	handler := &Handler{}
	handler.upgrades.begin()
	defer handler.upgrades.end()
	ctx, cancel := context.WithCancel(context.Background())
	stream := &prepareRestartStreamStub{ctx: ctx}
	done := make(chan error, 1)
	go func() {
		done <- handler.PrepareRestart(&agentpb.Empty{}, stream)
	}()
	time.Sleep(1300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("PrepareRestart did not stop after context cancellation")
	}
	for _, state := range stream.states {
		if state == "ready" {
			t.Fatal("expected no ready state after cancellation")
		}
	}
}
