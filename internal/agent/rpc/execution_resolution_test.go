package rpc

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	contract "panel/internal/agent/contract"
	"panel/internal/agent/executionevents"
	pb "panel/internal/agent/pb"
)

func TestManualExecutionResolutionRPCKeepsDeclarationMetadata(t *testing.T) {
	ctx := context.Background()
	spool, err := executionevents.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer spool.Close()
	session, _, err := spool.Begin(ctx, contract.RuntimeReconcileRequest{ExecutionID: "execution", OperationID: "operation", RunID: "run", InstanceID: "instance", Action: "purge", RemoveData: true})
	if err != nil {
		t.Fatal(err)
	}
	session.Abandon()
	h := &Handler{eventStore: spool}
	invalid := &pb.ExecutionResolutionRequest{ExecutionId: "execution", Outcome: "succeeded"}
	if _, err = h.ResolveExecution(ctx, invalid); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing actor/reason accepted: %v", err)
	}
	response, err := h.ResolveExecution(ctx, &pb.ExecutionResolutionRequest{ExecutionId: "execution", Outcome: "succeeded", Reason: "Checked the node", ActorId: "admin", ActorName: "Administrator"})
	if err != nil {
		t.Fatal(err)
	}
	converted := GoRuntimeReconcileResponse(response.Result)
	if response.State != "finished" || converted.VerificationSource != "manual" || converted.VerificationReason != "Checked the node" || converted.VerifiedBy != "admin" || converted.VerifiedAt == nil || !converted.ObservedAt.IsZero() {
		t.Fatalf("manual metadata lost in RPC: %+v", converted)
	}
}
