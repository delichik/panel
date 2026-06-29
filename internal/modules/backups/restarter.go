package backups

import (
	"os"
	"strings"
	"time"
)

type Restarter interface {
	Supported() bool
	RestartSoon()
}

type noopRestarter struct{}

func (noopRestarter) Supported() bool { return false }
func (noopRestarter) RestartSoon()    {}

type processRestarter struct {
	delay    time.Duration
	stat     func(string) (os.FileInfo, error)
	readFile func(string) ([]byte, error)
	exit     func(int)
}

func NewContainerRestarter() Restarter {
	return processRestarter{
		delay:    800 * time.Millisecond,
		stat:     os.Stat,
		readFile: os.ReadFile,
		exit:     os.Exit,
	}
}

func (r processRestarter) Supported() bool {
	if r.stat == nil || r.readFile == nil {
		return false
	}
	if _, err := r.stat("/.dockerenv"); err == nil {
		return true
	}
	raw, err := r.readFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	cgroup := strings.ToLower(string(raw))
	return strings.Contains(cgroup, "docker") ||
		strings.Contains(cgroup, "containerd") ||
		strings.Contains(cgroup, "kubepods")
}

func (r processRestarter) RestartSoon() {
	if !r.Supported() || r.exit == nil {
		return
	}
	delay := r.delay
	if delay <= 0 {
		delay = 800 * time.Millisecond
	}
	go func() {
		time.Sleep(delay)
		r.exit(0)
	}()
}
