package applications

import (
	"encoding/json"
	"testing"

	"panel/internal/modules/tasks"
)

func targetTaskInputForTest(t *testing.T, target LifecycleTarget, summary, triggerType string) tasks.CreateInput {
	t.Helper()
	input, err := targetTaskInput(target, summary, triggerType)
	if err != nil {
		t.Fatal(err)
	}
	return input
}

func targetTaskInput(target LifecycleTarget, summary, triggerType string) (tasks.CreateInput, error) {
	return targetTaskInputWithRemoveData(target, summary, triggerType, false)
}

func targetTaskInputWithRemoveData(target LifecycleTarget, summary, triggerType string, removeApplicationData bool) (tasks.CreateInput, error) {
	params, err := json.Marshal(deployTaskParams{
		AppID:                 target.ApplicationID,
		ServerID:              target.ServerID,
		LifecycleOperationID:  target.OperationID,
		LifecycleTargetID:     target.ID,
		Generation:            target.DesiredGeneration,
		SpecHash:              target.DesiredSpecHash,
		Action:                target.Action,
		Purge:                 target.Action == LifecycleTargetActionPurge,
		RemoveApplicationData: target.Action == LifecycleTargetActionPurge && removeApplicationData,
	})
	if err != nil {
		return tasks.CreateInput{}, err
	}
	metadata, err := json.Marshal(map[string]any{
		"applicationId":        target.ApplicationID,
		"serverId":             target.ServerID,
		"action":               target.Action,
		"generation":           target.DesiredGeneration,
		"specHash":             target.DesiredSpecHash,
		"lifecycleOperationId": target.OperationID,
		"lifecycleTargetId":    target.ID,
	})
	if err != nil {
		return tasks.CreateInput{}, err
	}
	return tasks.CreateInput{
		Type:         targetTaskTypeForAction(target.Action),
		ServerID:     target.ServerID,
		ResourceType: "application",
		ResourceID:   target.ApplicationID,
		TriggerType:  triggerType,
		ParamsJSON:   string(params),
		MetadataJSON: string(metadata),
		Summary:      summary,
	}, nil
}
