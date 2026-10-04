package rpc

import (
	"context"
	"errors"
	"reflect"
	"testing"

	agentcontract "panel/internal/agent/contract"
	agentfirewall "panel/internal/agent/firewall"

	agentdocker "panel/internal/agent/docker"
	agentpb "panel/internal/agent/pb"
	appruntime "panel/internal/modules/applications/runtime"

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

func TestApplicationFirewallRulesSelectsOnlyFixedOpenMappings(t *testing.T) {
	rules := applicationFirewallRules(appruntime.Spec{Ports: []appruntime.Port{
		{HostPort: 8080, OpenFirewall: true},
		{HostPort: 5353, Protocol: "udp", OpenFirewall: true},
		{HostPort: 9090, Protocol: "tcp"},
		{ContainerPort: 3000, OpenFirewall: true},
	}})
	if len(rules) != 2 || rules[0].Port != 8080 || rules[0].Protocol != "" || rules[1].Port != 5353 || rules[1].Protocol != "udp" {
		t.Fatalf("applicationFirewallRules() = %#v", rules)
	}
}

type orderedFirewall struct {
	calls *[]string
	errOn string
}

func (f orderedFirewall) EnsureApplicationRules(context.Context, string, int, int, []agentfirewall.Rule) error {
	*f.calls = append(*f.calls, "ensure")
	if f.errOn == "ensure" {
		return errors.New("ensure failed")
	}
	return nil
}

func (f orderedFirewall) CleanupApplicationRules(context.Context, string, int, int, []agentfirewall.Rule) error {
	*f.calls = append(*f.calls, "cleanup")
	if f.errOn == "cleanup" {
		return errors.New("cleanup failed")
	}
	return nil
}

func TestReconcileRuntimeAndFirewallOrdering(t *testing.T) {
	for _, test := range []struct {
		name       string
		action     string
		runtimeErr error
		want       []string
	}{
		{name: "apply", action: "apply", want: []string{"ensure", "runtime", "cleanup"}},
		{name: "apply runtime failure", action: "apply", runtimeErr: errors.New("docker failed"), want: []string{"ensure", "runtime"}},
		{name: "stop", action: "stop", want: []string{"runtime", "cleanup"}},
		{name: "purge", action: "purge", want: []string{"runtime", "cleanup"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := []string{}
			handler := &Handler{
				firewall: orderedFirewall{calls: &calls},
				runtimeReconcile: func(context.Context, agentcontract.RuntimeReconcileRequest) (agentcontract.RuntimeReconcileResponse, error) {
					calls = append(calls, "runtime")
					return agentcontract.RuntimeReconcileResponse{ObservedState: "running"}, test.runtimeErr
				},
			}
			_, _ = handler.reconcileRuntimeAndFirewall(context.Background(), agentcontract.RuntimeReconcileRequest{
				ApplicationID: "app-a", Action: test.action, SSHPort: 22,
				Spec: appruntime.Spec{Ports: []appruntime.Port{{HostPort: 8080, OpenFirewall: true}}},
			})
			if !reflect.DeepEqual(calls, test.want) {
				t.Fatalf("calls = %#v, want %#v", calls, test.want)
			}
		})
	}
}

func TestReconcileRuntimeAndFirewallCleanupFailureIsRetryable(t *testing.T) {
	calls := []string{}
	handler := &Handler{
		firewall: orderedFirewall{calls: &calls, errOn: "cleanup"},
		runtimeReconcile: func(context.Context, agentcontract.RuntimeReconcileRequest) (agentcontract.RuntimeReconcileResponse, error) {
			calls = append(calls, "runtime")
			return agentcontract.RuntimeReconcileResponse{ObservedState: "stopped"}, nil
		},
	}
	result, err := handler.reconcileRuntimeAndFirewall(context.Background(), agentcontract.RuntimeReconcileRequest{ApplicationID: "app-a", Action: "stop", SSHPort: 22})
	if err == nil || result.ErrorCode != "firewall_reconcile_failed" || !result.Retryable {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}
