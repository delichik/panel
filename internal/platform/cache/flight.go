package cache

import (
	"context"
	"fmt"
	"sync"
)

// flightGroup coalesces concurrent loads of the same key.
type flightGroup struct {
	mu    sync.Mutex
	calls map[string]*flightCall
}

type flightCall struct {
	done chan struct{}
	val  []byte
	err  error
}

func newFlightGroup() *flightGroup {
	return &flightGroup{calls: make(map[string]*flightCall)}
}

// Do runs fn once for concurrent callers with the same key. Waiters receive a
// copy of the leader's result. A canceled waiter returns its context error
// without canceling the shared load.
func (g *flightGroup) Do(ctx context.Context, key string, fn func(context.Context) ([]byte, error)) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	g.mu.Lock()
	if call, ok := g.calls[key]; ok {
		g.mu.Unlock()
		select {
		case <-call.done:
			return cloneBytes(call.val), call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	call := &flightCall{done: make(chan struct{})}
	g.calls[key] = call
	g.mu.Unlock()

	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				call.val = nil
				call.err = fmt.Errorf("cache: load panicked: %v", recovered)
			}
		}()
		call.val, call.err = fn(ctx)
	}()

	g.mu.Lock()
	delete(g.calls, key)
	close(call.done)
	g.mu.Unlock()

	return cloneBytes(call.val), call.err
}
