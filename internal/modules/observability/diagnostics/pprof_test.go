package diagnostics

import (
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPprofServerEnableDisable(t *testing.T) {
	server := NewPprofServer("127.0.0.1:0")
	if status := server.Status(); status.Enabled {
		t.Fatalf("pprof enabled before Enable: %#v", status)
	}
	if err := server.Enable(); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	status := server.Status()
	if !status.Enabled {
		t.Fatalf("pprof not enabled after Enable: %#v", status)
	}
	if status.Address == "" || status.Address == "127.0.0.1:0" {
		t.Fatalf("pprof address not bound: %#v", status)
	}
	if err := server.Enable(); err != nil {
		t.Fatalf("Enable while already enabled: %v", err)
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + status.Address + "/debug/pprof/")
	if err != nil {
		t.Fatalf("GET /debug/pprof/: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "goroutine") {
		t.Fatalf("unexpected pprof index response: status=%d body=%q", resp.StatusCode, body)
	}

	if err := server.Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if status := server.Status(); status.Enabled {
		t.Fatalf("pprof still enabled after Disable: %#v", status)
	}
	if err := server.Disable(); err != nil {
		t.Fatalf("Disable while already disabled: %v", err)
	}
	client.CloseIdleConnections()
	if _, err := client.Get("http://" + status.Address + "/debug/pprof/"); err == nil {
		t.Fatal("pprof listener still accepting connections after Disable")
	}
}

func TestPprofServerEnableFailsWhenPortInUse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	server := NewPprofServer(listener.Addr().String())
	if err := server.Enable(); err == nil {
		t.Fatal("Enable succeeded while the port is already in use")
	}
	if status := server.Status(); status.Enabled {
		t.Fatalf("pprof marked enabled after failed Enable: %#v", status)
	}
}

func TestServicePprofDelegation(t *testing.T) {
	service := NewService()
	if status := service.PprofStatus(); status.Enabled {
		t.Fatalf("service pprof enabled before Enable: %#v", status)
	}
	defer service.Close()
	if err := service.EnablePprof(); err != nil {
		t.Fatalf("EnablePprof: %v", err)
	}
	if status := service.PprofStatus(); !status.Enabled {
		t.Fatalf("service pprof not enabled: %#v", status)
	}
	if err := service.DisablePprof(); err != nil {
		t.Fatalf("DisablePprof: %v", err)
	}
}
