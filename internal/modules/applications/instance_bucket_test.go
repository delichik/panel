package applications

import (
	"testing"

	appruntime "panel/internal/modules/applications/runtime"
	controlplane "panel/internal/orchestrator"
)

func TestClassifyInstanceBucket(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   instanceClassifyInput
		want string
	}{
		{
			name: "active job is deploying",
			in:   instanceClassifyInput{DesiredState: appruntime.DesiredRunning, ObservedStatus: appruntime.StatusRunning, HasActiveJob: true},
			want: InstanceBucketDeploying,
		},
		{
			name: "healthy running match",
			in: instanceClassifyInput{
				DesiredState: appruntime.DesiredRunning, ObservedStatus: appruntime.StatusRunning,
				DesiredGeneration: 2, ObservedGeneration: 2, DesiredSpecHash: "abc", ObservedSpecHash: "abc",
			},
			want: InstanceBucketHealthy,
		},
		{
			name: "generation mismatch abnormal",
			in: instanceClassifyInput{
				DesiredState: appruntime.DesiredRunning, ObservedStatus: appruntime.StatusRunning,
				DesiredGeneration: 3, ObservedGeneration: 2, DesiredSpecHash: "abc", ObservedSpecHash: "abc",
			},
			want: InstanceBucketAbnormal,
		},
		{
			name: "desired running missing abnormal",
			in:   instanceClassifyInput{DesiredState: appruntime.DesiredRunning, ObservedStatus: appruntime.StatusMissing},
			want: InstanceBucketAbnormal,
		},
		{
			name: "stop converged omitted while disabled",
			in:   instanceClassifyInput{DesiredState: appruntime.DesiredStopped, ObservedStatus: appruntime.StatusMissing},
			want: InstanceBucketOmitted,
		},
		{
			name: "stop converged abnormal while enabled",
			in:   instanceClassifyInput{DesiredState: appruntime.DesiredStopped, ObservedStatus: appruntime.StatusStopped, ApplicationEnabled: true},
			want: InstanceBucketAbnormal,
		},
		{
			name: "stop stuck running abnormal",
			in:   instanceClassifyInput{DesiredState: appruntime.DesiredStopped, ObservedStatus: appruntime.StatusRunning},
			want: InstanceBucketAbnormal,
		},
		{
			name: "purged row remaining abnormal",
			in:   instanceClassifyInput{DesiredState: controlplane.DesiredPurged, ObservedStatus: appruntime.StatusMissing},
			want: InstanceBucketAbnormal,
		},
		{
			name: "pending without job is abnormal not deploying",
			in:   instanceClassifyInput{DesiredState: appruntime.DesiredRunning, ObservedStatus: appruntime.StatusPending},
			want: InstanceBucketAbnormal,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyInstanceBucket(tc.in); got != tc.want {
				t.Fatalf("classify = %q, want %q", got, tc.want)
			}
		})
	}
}
