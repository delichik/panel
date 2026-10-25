// Package maintenance provides a reversible admission gate for runtime writers.
package maintenance

import (
	"context"
	"sync"
)

// Gate rejects new work while paused and drains previously admitted work.
// Rejection rather than blocking permits nested writers to unwind while a
// maintenance operation waits for their outer operation to finish.
type Gate struct {
	mu      sync.Mutex
	paused  bool
	active  int
	drained chan struct{}
}

func (g *Gate) Enter() (func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.paused {
		return nil, false
	}
	g.active++
	return func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.active--
		if g.active == 0 && g.drained != nil {
			close(g.drained)
			g.drained = nil
		}
	}, true
}

func (g *Gate) Pause() {
	_ = g.PauseContext(context.Background())
}

// BeginPause rejects new Enter calls immediately without waiting for in-flight
// work. Callers that still hold an Enter token must release it, then WaitDrained.
func (g *Gate) BeginPause() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.paused = true
	if g.active == 0 {
		return
	}
	if g.drained == nil {
		g.drained = make(chan struct{})
	}
}

// WaitDrained blocks until every Enter token released after BeginPause/Pause.
// On timeout the gate stays paused; the caller must Resume when abandoning.
func (g *Gate) WaitDrained(ctx context.Context) error {
	g.mu.Lock()
	if g.active == 0 {
		g.mu.Unlock()
		return nil
	}
	if g.drained == nil {
		g.drained = make(chan struct{})
	}
	drained := g.drained
	g.mu.Unlock()
	select {
	case <-drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// PauseContext leaves admission paused on timeout. The caller must resume it
// when abandoning maintenance; already admitted work is never interrupted.
func (g *Gate) PauseContext(ctx context.Context) error {
	g.BeginPause()
	return g.WaitDrained(ctx)
}

func (g *Gate) Resume() {
	g.mu.Lock()
	g.paused = false
	g.mu.Unlock()
}

func (g *Gate) Paused() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.paused
}
