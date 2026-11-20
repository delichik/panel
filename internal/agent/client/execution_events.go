package client

import (
	"context"
	"fmt"

	contract "panel/internal/agent/contract"
	pb "panel/internal/agent/pb"
	rpc "panel/internal/agent/rpc"
)

func (c *GRPCClient) requireExecutionEvents(ctx context.Context, endpoint string) error {
	health, err := c.Health(ctx, endpoint)
	if err != nil {
		return err
	}
	for _, capability := range health.Capabilities {
		if capability == contract.CapabilityExecutionEvents {
			return nil
		}
	}
	return fmt.Errorf("agent does not support %s; upgrade the Agent before changing remote runtime state", contract.CapabilityExecutionEvents)
}
func (c *GRPCClient) ReadExecutionEvents(ctx context.Context, endpoint string, req contract.ExecutionEventsRequest) (contract.ExecutionEventsResponse, error) {
	response, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client pb.AgentServiceClient) (*pb.ExecutionEventsResponse, error) {
		return client.ReadExecutionEvents(ctx, &pb.ExecutionEventsRequest{ExecutionId: req.ExecutionID, SourceEpoch: req.SourceEpoch, SourceStreamId: req.SourceStreamID, AfterSourceSeq: req.AfterSourceSeq, Limit: int32(req.Limit)})
	})
	if err != nil {
		return contract.ExecutionEventsResponse{}, err
	}
	out := contract.ExecutionEventsResponse{SourceID: response.SourceId, SourceEpoch: response.SourceEpoch, SourceStreamID: response.SourceStreamId, HeadSeq: response.HeadSeq, EndSeq: response.EndSeq, HasMore: response.HasMore, AckedThroughSeq: response.AckedThroughSeq}
	for _, event := range response.Events {
		out.Events = append(out.Events, rpc.GoExecutionEvent(event))
	}
	return out, nil
}
func (c *GRPCClient) AckExecutionEvents(ctx context.Context, endpoint string, req contract.ExecutionEventsAck) error {
	_, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client pb.AgentServiceClient) (*pb.OKResponse, error) {
		return client.AckExecutionEvents(ctx, &pb.ExecutionEventsAck{ExecutionId: req.ExecutionID, SourceId: req.SourceID, SourceEpoch: req.SourceEpoch, SourceStreamId: req.SourceStreamID, ThroughSourceSeq: req.ThroughSourceSeq})
	})
	return err
}
func (c *GRPCClient) GetExecutionResult(ctx context.Context, endpoint, executionID string) (contract.ExecutionResult, error) {
	response, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client pb.AgentServiceClient) (*pb.ExecutionResultResponse, error) {
		return client.GetExecutionResult(ctx, &pb.ExecutionResultRequest{ExecutionId: executionID})
	})
	if err != nil {
		return contract.ExecutionResult{}, err
	}
	out := contract.ExecutionResult{State: response.State, Error: response.Error, EndSeq: response.EndSeq}
	if response.Result != nil {
		result := rpc.GoRuntimeReconcileResponse(response.Result)
		out.Result = &result
	}
	return out, nil
}

var _ contract.ExecutionEventsClient = (*GRPCClient)(nil)

func (c *GRPCClient) ResolveExecution(ctx context.Context, endpoint string, in contract.ExecutionResolution) (contract.ExecutionResult, error) {
	health, err := c.Health(ctx, endpoint)
	if err != nil {
		return contract.ExecutionResult{}, err
	}
	supported := false
	for _, capability := range health.Capabilities {
		if capability == contract.CapabilityExecutionResolution {
			supported = true
			break
		}
	}
	if !supported {
		return contract.ExecutionResult{}, fmt.Errorf("agent does not support %s; upgrade the Agent before manual execution verification", contract.CapabilityExecutionResolution)
	}
	response, err := callRPC(c, ctx, endpoint, c.timeout, func(ctx context.Context, client pb.AgentServiceClient) (*pb.ExecutionResultResponse, error) {
		return client.ResolveExecution(ctx, &pb.ExecutionResolutionRequest{ExecutionId: in.ExecutionID, Outcome: in.Outcome, Reason: in.Reason, ActorId: in.ActorID, ActorName: in.ActorName})
	})
	if err != nil {
		return contract.ExecutionResult{}, err
	}
	out := contract.ExecutionResult{State: response.State, Error: response.Error, EndSeq: response.EndSeq}
	if response.Result != nil {
		result := rpc.GoRuntimeReconcileResponse(response.Result)
		out.Result = &result
	}
	return out, nil
}

var _ contract.ExecutionResolutionClient = (*GRPCClient)(nil)
