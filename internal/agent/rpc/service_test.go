package rpc

import (
	"context"
	"testing"

	agentdocker "panel/internal/agent/docker"
	agentpb "panel/internal/agent/pb"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRequireRuntimeReturnsFailedPrecondition(t *testing.T) {
	err := (&Handler{}).requireRuntime()
	if got, want := status.Code(err), codes.FailedPrecondition; got != want {
		t.Fatalf("status.Code(requireRuntime()) = %v, want %v", got, want)
	}
}

func TestDockerContainerActionRejectsUnknownAction(t *testing.T) {
	handler := &Handler{runtime: &agentdocker.LocalRuntime{}}

	_, err := handler.DockerContainerAction(context.Background(), &agentpb.DockerContainerActionRequest{
		Id:     "container-1",
		Action: "pause",
	})
	if got, want := status.Code(err), codes.InvalidArgument; got != want {
		t.Fatalf("status.Code(DockerContainerAction unknown action) = %v, want %v", got, want)
	}
}
