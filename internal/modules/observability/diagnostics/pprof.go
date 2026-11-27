package diagnostics

import (
	"net"
	"net/http"
	"net/http/pprof"
	"sync"
	"time"
)

// DefaultPprofAddress is the loopback-only address used for the runtime pprof
// server. It is intentionally bound to 127.0.0.1 so profiling data is never
// exposed through the panel's public interface.
const DefaultPprofAddress = "127.0.0.1:6060"

type PprofStatus struct {
	Enabled bool   `json:"enabled"`
	Address string `json:"address"`
}

type PprofUpdate struct {
	Enabled bool `json:"enabled"`
}

// PprofServer manages an optional net/http/pprof listener. It is safe for
// concurrent use.
type PprofServer struct {
	mu       sync.Mutex
	address  string
	server   *http.Server
	listener net.Listener
}

func NewPprofServer(address string) *PprofServer {
	if address == "" {
		address = DefaultPprofAddress
	}
	return &PprofServer{address: address}
}

func (p *PprofServer) Status() PprofStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	address := p.address
	if p.listener != nil {
		address = p.listener.Addr().String()
	}
	return PprofStatus{Enabled: p.server != nil, Address: address}
}

// Enable starts the pprof listener. It is idempotent: enabling an already
// running server is a no-op.
func (p *PprofServer) Enable() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.server != nil {
		return nil
	}
	listener, err := net.Listen("tcp", p.address)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	p.listener = listener
	p.server = server
	go func() {
		_ = server.Serve(listener)
	}()
	return nil
}

// Disable stops the pprof listener. It is idempotent.
func (p *PprofServer) Disable() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.server == nil {
		return nil
	}
	err := p.server.Close()
	p.server = nil
	p.listener = nil
	return err
}
