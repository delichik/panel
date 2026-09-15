package rpc

import (
	"context"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	contract "panel/internal/agent/contract"
	"panel/internal/agent/executionevents"
	pb "panel/internal/agent/pb"
)

func (h *Handler) executionEvents() (*executionevents.Store, error) {
	h.eventOnce.Do(func() {
		if h.eventStore != nil {
			return
		}
		if h.eventDir == "" {
			h.eventErr = errors.New("execution event directory is not configured")
			return
		}
		h.eventStore, h.eventErr = executionevents.Open(h.eventDir)
	})
	return h.eventStore, h.eventErr
}

func (h *Handler) ReadExecutionEvents(ctx context.Context, req *pb.ExecutionEventsRequest) (*pb.ExecutionEventsResponse, error) {
	store, err := h.executionEvents()
	if err != nil {
		return nil, remoteError(err)
	}
	out, err := store.Read(ctx, contract.ExecutionEventsRequest{ExecutionID: req.ExecutionId, SourceEpoch: req.SourceEpoch, SourceStreamID: req.SourceStreamId, AfterSourceSeq: req.AfterSourceSeq, Limit: int(req.Limit)})
	if errors.Is(err, executionevents.ErrNotFound) {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	if err != nil {
		return nil, remoteError(err)
	}
	response := &pb.ExecutionEventsResponse{SourceId: out.SourceID, SourceEpoch: out.SourceEpoch, SourceStreamId: out.SourceStreamID, HeadSeq: out.HeadSeq, EndSeq: out.EndSeq, HasMore: out.HasMore, AckedThroughSeq: out.AckedThroughSeq}
	for _, event := range out.Events {
		response.Events = append(response.Events, PBExecutionEvent(event))
	}
	return response, nil
}
func (h *Handler) AckExecutionEvents(ctx context.Context, req *pb.ExecutionEventsAck) (*pb.OKResponse, error) {
	store, err := h.executionEvents()
	if err != nil {
		return nil, remoteError(err)
	}
	err = store.Ack(ctx, contract.ExecutionEventsAck{ExecutionID: req.ExecutionId, SourceID: req.SourceId, SourceEpoch: req.SourceEpoch, SourceStreamID: req.SourceStreamId, ThroughSourceSeq: req.ThroughSourceSeq})
	if err != nil {
		return nil, remoteError(err)
	}
	return &pb.OKResponse{Ok: true}, nil
}
func (h *Handler) GetExecutionResult(ctx context.Context, req *pb.ExecutionResultRequest) (*pb.ExecutionResultResponse, error) {
	store, err := h.executionEvents()
	if err != nil {
		return nil, remoteError(err)
	}
	out, err := store.Result(ctx, req.ExecutionId)
	if err != nil {
		return nil, remoteError(err)
	}
	if out.State == "unknown" && h.runtime != nil {
		session, recoveryErr := store.RecoverySession(ctx, req.ExecutionId)
		if recoveryErr != nil {
			return nil, remoteError(recoveryErr)
		}
		if session != nil {
			result, matched, verifyErr := h.runtime.VerifyExecution(ctx, session.Request())
			if verifyErr == nil && matched {
				if err = session.Append(ctx, contract.ExecutionEvent{EventType: "verification.finished", Status: "succeeded", Text: "Recovered the expected runtime state after an interrupted execution"}); err != nil {
					return nil, remoteError(err)
				}
				if err = session.Finish(ctx, result, nil); err != nil {
					return nil, remoteError(err)
				}
				out, err = store.Result(ctx, req.ExecutionId)
				if err != nil {
					return nil, remoteError(err)
				}
			}
		}
	}
	response := &pb.ExecutionResultResponse{State: out.State, Error: out.Error, EndSeq: out.EndSeq}
	if out.Result != nil {
		response.Result = PBRuntimeReconcileResponse(*out.Result)
	}
	return response, nil
}
func PBExecutionEvent(event contract.ExecutionEvent) *pb.ExecutionEvent {
	return &pb.ExecutionEvent{EventId: event.EventID, SourceSeq: event.SourceSeq, OccurredAt: timestamppb.New(event.OccurredAt), EventType: event.EventType, OperationId: event.OperationID, RunId: event.RunID, ExecutionId: event.ExecutionID, StepId: event.StepID, StepName: event.StepName, Status: event.Status, Stream: event.Stream, Text: event.Text, DataJson: event.DataJSON}
}
func GoExecutionEvent(event *pb.ExecutionEvent) contract.ExecutionEvent {
	if event == nil {
		return contract.ExecutionEvent{}
	}
	out := contract.ExecutionEvent{EventID: event.EventId, SourceSeq: event.SourceSeq, EventType: event.EventType, OperationID: event.OperationId, RunID: event.RunId, ExecutionID: event.ExecutionId, StepID: event.StepId, StepName: event.StepName, Status: event.Status, Stream: event.Stream, Text: event.Text, DataJSON: event.DataJson}
	if event.OccurredAt != nil {
		out.OccurredAt = event.OccurredAt.AsTime()
	}
	return out
}
func optionalGoTime(value *timestamppb.Timestamp) *time.Time {
	if value == nil {
		return nil
	}
	t := value.AsTime()
	return &t
}
func optionalPBTime(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(*value)
}

// ResolveExecution is protected by the Agent service's existing Panel mTLS
// listener. Actor fields describe the Panel-authenticated human declaration;
// this RPC does not expose a generic edit or runtime command facility.
func (h *Handler) ResolveExecution(ctx context.Context, req *pb.ExecutionResolutionRequest) (*pb.ExecutionResultResponse, error) {
	store, err := h.executionEvents()
	if err != nil {
		return nil, remoteError(err)
	}
	result, err := store.Resolve(ctx, contract.ExecutionResolution{ExecutionID: req.ExecutionId, Outcome: req.Outcome, Reason: req.Reason, ActorID: req.ActorId, ActorName: req.ActorName})
	switch {
	case errors.Is(err, executionevents.ErrResolutionInvalid):
		return nil, status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, executionevents.ErrResolutionState), errors.Is(err, executionevents.ErrConflict):
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, executionevents.ErrNotFound):
		return nil, status.Error(codes.NotFound, err.Error())
	case err != nil:
		return nil, remoteError(err)
	}
	response := &pb.ExecutionResultResponse{State: result.State, Error: result.Error, EndSeq: result.EndSeq}
	if result.Result != nil {
		response.Result = PBRuntimeReconcileResponse(*result.Result)
	}
	return response, nil
}
