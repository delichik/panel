package backups

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestProcessRestarterDetectsDockerEnv(t *testing.T) {
	r := processRestarter{
		stat: func(path string) (os.FileInfo, error) {
			if path == "/.dockerenv" {
				return fakeFileInfo{}, nil
			}
			return nil, os.ErrNotExist
		},
		readFile: func(string) ([]byte, error) { return nil, os.ErrNotExist },
		exit:     func(int) {},
	}
	if !r.Supported() {
		t.Fatal("expected /.dockerenv to mark restart supported")
	}
}

func TestProcessRestarterDetectsContainerCgroup(t *testing.T) {
	r := processRestarter{
		stat:     func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		readFile: func(string) ([]byte, error) { return []byte("0::/system.slice/docker-abc.scope"), nil },
		exit:     func(int) {},
	}
	if !r.Supported() {
		t.Fatal("expected docker cgroup to mark restart supported")
	}
}

func TestProcessRestarterUnsupportedOutsideContainer(t *testing.T) {
	r := processRestarter{
		stat:     func(string) (os.FileInfo, error) { return nil, os.ErrNotExist },
		readFile: func(string) ([]byte, error) { return []byte("0::/init.scope"), nil },
		exit:     func(int) {},
	}
	if r.Supported() {
		t.Fatal("expected non-container cgroup to be unsupported")
	}
}

func TestProcessRestarterRestartSoonExitsAfterDelay(t *testing.T) {
	exited := make(chan int, 1)
	r := processRestarter{
		delay:    time.Millisecond,
		stat:     func(string) (os.FileInfo, error) { return fakeFileInfo{}, nil },
		readFile: func(string) ([]byte, error) { return nil, errors.New("unused") },
		exit:     func(code int) { exited <- code },
	}
	r.RestartSoon()
	select {
	case code := <-exited:
		if code != 0 {
			t.Fatalf("expected exit code 0, got %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("expected delayed exit")
	}
}

type fakeFileInfo struct{}

func (fakeFileInfo) Name() string       { return ".dockerenv" }
func (fakeFileInfo) Size() int64        { return 0 }
func (fakeFileInfo) Mode() os.FileMode  { return 0 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() any           { return nil }
