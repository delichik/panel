package client

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
	contract "panel/internal/agent/contract"
	pb "panel/internal/agent/pb"
)

type resolutionServerStub struct {
	pb.UnimplementedAgentServiceServer
	supported bool
	request   *pb.ExecutionResolutionRequest
}

func (s *resolutionServerStub) Health(context.Context, *pb.Empty) (*pb.HealthResponse, error) {
	capabilities := []string{contract.CapabilityExecutionEvents}
	if s.supported {
		capabilities = append(capabilities, contract.CapabilityExecutionResolution)
	}
	return &pb.HealthResponse{Status: "ok", Capabilities: capabilities}, nil
}
func (s *resolutionServerStub) ResolveExecution(_ context.Context, req *pb.ExecutionResolutionRequest) (*pb.ExecutionResultResponse, error) {
	s.request = req
	return &pb.ExecutionResultResponse{State: "finished", EndSeq: 9, Result: &pb.RuntimeReconcileResponse{ObservedState: "unknown", VerificationSource: "manual", VerificationReason: req.Reason, VerifiedBy: req.ActorId, VerifiedByName: req.ActorName, VerifiedAt: timestamppb.New(time.Now().UTC())}}, nil
}
func TestManualResolutionClientUsesCapabilityAndMTLSTransport(t *testing.T) {
	for _, supported := range []bool{false, true} {
		t.Run(map[bool]string{false: "old_agent", true: "supported_agent"}[supported], func(t *testing.T) {
			server := &resolutionServerStub{supported: supported}
			client, endpoint, closeServer := newPrepareRestartTestClient(t, server)
			defer closeServer()
			result, err := client.ResolveExecution(context.Background(), endpoint, contract.ExecutionResolution{ExecutionID: "execution", Outcome: "succeeded", Reason: "Checked node data", ActorID: "admin", ActorName: "Administrator"})
			if !supported {
				if err == nil || server.request != nil {
					t.Fatalf("old Agent received a manual mutation: %v %+v", err, server.request)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if server.request == nil || server.request.ExecutionId != "execution" || server.request.Outcome != "succeeded" || result.EndSeq != 9 || result.Result.VerificationSource != "manual" || result.Result.VerifiedBy != "admin" || result.Result.VerifiedAt == nil {
				t.Fatalf("declaration lost in transport: %+v %+v", server.request, result)
			}
		})
	}
}
