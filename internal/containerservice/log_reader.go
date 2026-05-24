package containerservice

import (
	"context"
	"fmt"
	"strings"
	"time"

	"panel/internal/server"
	"panel/internal/sshx"
)

type DockerLogReader struct {
	servers *server.Service
	exec    sshx.RemoteExecutor
}

func NewDockerLogReader(servers *server.Service, exec sshx.RemoteExecutor) *DockerLogReader {
	return &DockerLogReader{servers: servers, exec: exec}
}

func (r *DockerLogReader) ReadContainerLogs(ctx context.Context, nodeID, containerName string, tail int) ([]string, error) {
	node, err := r.servers.Get(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if tail <= 0 {
		tail = 200
	}
	cmd := fmt.Sprintf("docker logs --tail %d %s 2>&1", tail, shellQuote(containerName))
	result, err := r.exec.ExecSudo(ctx, node.Target(), sshx.CommandSpec{Command: cmd, Timeout: time.Minute})
	if err != nil {
		return nil, err
	}
	out := strings.TrimRight(result.Stdout, "\r\n")
	if out == "" {
		return []string{}, nil
	}
	return strings.Split(out, "\n"), nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
