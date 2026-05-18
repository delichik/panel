package docker

import (
	"context"

	"panel/internal/sshx"
)

type ComposeProject struct {
	Name string
}

type ComposeStatus struct {
	Project string
	State   string
}

type ContainerRuntime interface {
	ListProjects(ctx context.Context, target sshx.Target) ([]ComposeProject, error)
	ReadStatus(ctx context.Context, target sshx.Target, project string) (ComposeStatus, error)
}
