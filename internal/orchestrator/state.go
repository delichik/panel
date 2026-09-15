package orchestrator

import "fmt"

type Transition struct {
	From  string
	Event string
	To    string
}

func transition(from, event string) (string, error) {
	switch from + ":" + event {
	case JobPending + ":claim":
		return JobRunning, nil
	case JobPending + ":cancel":
		return JobCancelled, nil
	case JobRunning + ":succeed":
		return JobSucceeded, nil
	case JobRunning + ":fail_retryable":
		return JobFailedRetryable, nil
	case JobRunning + ":fail_terminal":
		return JobFailed, nil
	case JobRunning + ":cancel":
		return JobCancelled, nil
	case JobFailedRetryable + ":backoff_due":
		return JobPending, nil
	case JobFailedRetryable + ":cancel":
		return JobCancelled, nil
	default:
		return "", fmt.Errorf("invalid job transition: %s -> %s", from, event)
	}
}
