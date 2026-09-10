package activitylog

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"modernc.org/sqlite"
	"strings"
)

// Functions are registered during package initialization, before any SQLite
// connection is opened. They apply the same redaction boundary to raw SQL
// control writes and Go producers.
func init() {
	sqlite.MustRegisterDeterministicScalarFunction("activity_redact", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		if args[0] == nil {
			return "", nil
		}
		return Redact(fmt.Sprint(args[0])), nil
	})
	sqlite.MustRegisterDeterministicScalarFunction("activity_redact_json", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		var value any
		if err := json.Unmarshal([]byte(fmt.Sprint(args[0])), &value); err != nil {
			return nil, err
		}
		out, err := json.Marshal(redactValue(value))
		return string(out), err
	})
	sqlite.MustRegisterDeterministicScalarFunction("activity_hash", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		digest := sha256.Sum256([]byte(fmt.Sprint(args[0])))
		return hex.EncodeToString(digest[:]), nil
	})
}

// InstallControlTriggers records control transitions in the same SQLite
// transaction that accepts them. Queue rows remain mutable; these facts do not.
func InstallControlTriggers(ctx context.Context, db *sql.DB) error {
	has := func(table string) bool {
		var n int
		return db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n) == nil && n > 0
	}
	name := func(table, expr string) string {
		if !has(table) {
			return `''`
		}
		return `(SELECT COALESCE(name,'') FROM ` + table + ` WHERE id=` + expr + `)`
	}
	taskResources := `json_array(json_object('resourceType',COALESCE(NULLIF(NEW.resource_type,''),'task'),'resourceId',COALESCE(NULLIF(NEW.resource_id,''),NEW.id),'role','target','nameSnapshot',COALESCE(` + name("applications", "NEW.resource_id") + `,` + name("servers", "NEW.resource_id") + `,'')),json_object('resourceType','server','resourceId',NEW.server_id,'role','executor','nameSnapshot',COALESCE(` + name("servers", "NEW.server_id") + `,'')))`
	// Manual retries inherit their original logical execution identity from
	// the preceding immutable request, while automatic retries keep task ID.
	taskLogical := `CASE WHEN NEW.trigger_type='retry' AND NEW.trigger_task_id<>'' THEN COALESCE((SELECT json_extract(data_json,'$.logicalExecutionId') FROM activity_events WHERE operation_id=NEW.operation_id AND run_id=NEW.trigger_task_id AND event_type IN ('operation.requested','retry.requested') ORDER BY seq LIMIT 1),NEW.trigger_task_id) ELSE NEW.id END`
	taskData := `json_object('logicalExecutionId',` + taskLogical + `,'isAggregate',NEW.child_count>0,'taskId',NEW.id,'parentTaskId',NEW.parent_task_id,'attempt',NEW.retry_count+1,'status',NEW.status,'stage',NEW.stage,'error',NEW.error,'uncertainty',NEW.stage='uncertain','phase',CASE WHEN NEW.stage='uncertain' THEN 'waiting' WHEN NEW.status='running' THEN 'running' WHEN NEW.status='failed_retryable' THEN 'waiting' WHEN NEW.status IN ('queued','scheduled') THEN 'queued' ELSE 'ended' END,'result',CASE NEW.status WHEN 'completed' THEN 'succeeded' WHEN 'failed' THEN 'failed' WHEN 'blocked' THEN 'failed' WHEN 'cancelled' THEN 'cancelled' ELSE NULL END)`
	taskBase := map[string]string{"operation_id": "NEW.operation_id", "run_id": "NEW.id", "execution_id": "NEW.id || ':attempt:' || NEW.retry_count", "domain": `CASE WHEN NEW.resource_type<>'' THEN NEW.resource_type ELSE 'system' END`, "action": "NEW.type", "trigger": "NEW.trigger_type", "actor_id": "NEW.triggered_by", "actor_kind": `CASE WHEN NEW.triggered_by<>'' THEN 'user' ELSE 'system' END`, "resources_json": taskResources, "text": "NEW.summary", "data_json": taskData}
	if has("auth_accounts") {
		taskBase["actor_name"] = `COALESCE((SELECT username FROM auth_accounts WHERE id=NEW.triggered_by),'')`
	}
	taskBase["initiator_json"] = `json_object('kind',CASE WHEN NEW.triggered_by<>'' THEN 'user' ELSE 'system' END,'id',NEW.triggered_by)`
	install := func(table, name, when, condition string, fields map[string]string) error {
		if !has(table) {
			return nil
		}
		statement := `CREATE TRIGGER IF NOT EXISTS ` + name + ` AFTER ` + when + ` ON ` + table
		if condition != "" {
			statement += ` WHEN ` + condition
		}
		statement += ` BEGIN ` + controlInsert(fields) + `; END`
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("activity control %s: %w", name, err)
		}
		return nil
	}
	merge := func(base map[string]string, extra map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	if err := install("tasks", "activity_task_requested", "INSERT", "", merge(taskBase, map[string]string{"event_type": `CASE WHEN NEW.trigger_type='retry' THEN 'retry.requested' ELSE 'operation.requested' END`, "kind": `'request'`})); err != nil {
		return err
	}
	if err := install("tasks", "activity_task_transition", "UPDATE", `OLD.status<>NEW.status OR OLD.stage<>NEW.stage OR OLD.triggered_by<>NEW.triggered_by`, merge(taskBase, map[string]string{"execution_id": `NEW.id || ':attempt:' || CASE WHEN NEW.status='failed_retryable' THEN OLD.retry_count ELSE NEW.retry_count END`, "data_json": `json_patch(` + taskData + `,json_object('attempt',CASE WHEN NEW.status='failed_retryable' THEN OLD.retry_count+1 ELSE NEW.retry_count+1 END))`, "event_type": `CASE WHEN NEW.stage='uncertain' THEN 'uncertainty.detected' WHEN OLD.status=NEW.status THEN 'execution.progress' WHEN NEW.status='running' THEN 'execution.started' WHEN NEW.status='failed_retryable' THEN 'retry.scheduled' WHEN NEW.status IN ('queued','scheduled') THEN 'execution.queued' ELSE 'execution.finished' END`, "level": `CASE WHEN NEW.status IN ('failed','failed_retryable','blocked') THEN 'error' ELSE 'info' END`})); err != nil {
		return err
	}
	if err := install("tasks", "activity_task_source_lost", "UPDATE", `NEW.stage='uncertain' AND OLD.stage<>'uncertain'`, merge(taskBase, map[string]string{"event_type": `'evidence.gap_detected'`, "kind": `'integrity'`, "level": `'warning'`, "text": `'The source process was lost; committed output remains available but uncommitted output cannot be verified'`, "data_json": `json_patch(` + taskData + `,json_object('gapId',NEW.id || ':attempt:' || NEW.retry_count,'reason','source_process_lost','committedOutputPreserved',json('true')))`})); err != nil {
		return err
	}
	// Step IDs are scoped to an execution attempt, so retried same-name steps do
	// not overwrite the meaning of a prior attempt in the immutable timeline.
	stepBase := map[string]string{"operation_id": `(SELECT operation_id FROM tasks WHERE id=NEW.task_id)`, "run_id": "NEW.task_id", "execution_id": `NEW.task_id || ':attempt:' || (SELECT retry_count FROM tasks WHERE id=NEW.task_id)`, "step_id": `NEW.id || ':attempt:' || (SELECT retry_count FROM tasks WHERE id=NEW.task_id)`, "action": `(SELECT type FROM tasks WHERE id=NEW.task_id)`, "event_type": `CASE WHEN NEW.status='running' THEN 'step.started' ELSE 'step.finished' END`, "level": `CASE WHEN NEW.status='failed' THEN 'error' ELSE 'info' END`, "text": "NEW.step", "data_json": `json_object('taskId',NEW.task_id,'step',NEW.step,'status',NEW.status,'error',NEW.error,'startedAt',NEW.started_at,'finishedAt',NEW.finished_at)`}
	for _, verb := range []string{"INSERT", "UPDATE"} {
		if err := install("task_steps", "activity_step_"+strings.ToLower(verb), verb, "", stepBase); err != nil {
			return err
		}
	}
	if err := install("task_logs", "activity_task_output", "INSERT", "", map[string]string{"event_type": `'output.chunk'`, "kind": `'output'`, "operation_id": `COALESCE((SELECT operation_id FROM tasks WHERE id=NEW.task_id),'')`, "run_id": "NEW.task_id", "execution_id": `NEW.task_id || ':attempt:' || COALESCE((SELECT retry_count FROM tasks WHERE id=NEW.task_id),0)`, "stream": "NEW.stream", "level": `CASE NEW.stream WHEN 'stderr' THEN 'error' ELSE 'info' END`, "text": "NEW.line", "occurred_at": "NEW.time", "data_json": `json_object('taskId',NEW.task_id)`}); err != nil {
		return err
	}
	jobResources := `json_array(json_object('resourceType','application','resourceId',NEW.application_id,'role','target','nameSnapshot',COALESCE(` + name("applications", "NEW.application_id") + `,''),'revisionId',NEW.desired_revision_id,'generation',NEW.desired_generation),json_object('resourceType','server','resourceId',NEW.server_id,'role','executor','nameSnapshot',COALESCE(` + name("servers", "NEW.server_id") + `,'')))`
	jobData := `json_object('logicalExecutionId',NEW.id,'jobId',NEW.id,'instanceId',NEW.instance_id,'attempt',NEW.attempts,'status',NEW.state,'stage',NEW.last_stage,'error',NEW.error_message,'errorCode',NEW.error_code,'errorClass',NEW.error_class,'detail',NEW.error_detail,'steps',json(NEW.last_steps_json),'desiredGeneration',NEW.desired_generation,'desiredRevisionId',NEW.desired_revision_id,'phase',CASE WHEN NEW.error_class='uncertainty' THEN 'waiting' WHEN NEW.state='running' THEN 'running' WHEN NEW.state='failed_retryable' THEN 'waiting' WHEN NEW.state='pending' THEN 'queued' ELSE 'ended' END,'result',CASE NEW.state WHEN 'succeeded' THEN 'succeeded' WHEN 'failed' THEN 'failed' WHEN 'cancelled' THEN 'cancelled' ELSE NULL END,'uncertainty',NEW.error_class='uncertainty')`
	jobBase := map[string]string{"operation_id": "NEW.intent_id", "run_id": "NEW.intent_id", "execution_id": "NEW.execution_id", "domain": `'application'`, "action": "NEW.action", "trigger": "NEW.trigger_type", "resources_json": jobResources, "actor_kind": `'controller'`, "text": "NEW.reason", "data_json": jobData}
	if err := install("jobs", "activity_job_requested", "INSERT", "", merge(jobBase, map[string]string{"event_type": `'execution.queued'`, "kind": `'lifecycle'`})); err != nil {
		return err
	}
	if err := install("jobs", "activity_job_transition", "UPDATE", `OLD.state<>NEW.state OR OLD.execution_id<>NEW.execution_id OR OLD.error_code<>NEW.error_code OR OLD.last_stage<>NEW.last_stage`, merge(jobBase, map[string]string{"event_type": `CASE WHEN NEW.error_class='uncertainty' THEN 'uncertainty.detected' WHEN NEW.state='running' THEN 'execution.started' WHEN NEW.state='failed_retryable' THEN 'retry.scheduled' WHEN NEW.state='pending' THEN 'execution.queued' ELSE 'execution.finished' END`, "level": `CASE WHEN NEW.state IN ('failed','failed_retryable') THEN 'error' WHEN NEW.error_class='uncertainty' THEN 'warning' ELSE 'info' END`})); err != nil {
		return err
	}
	if err := install("jobs", "activity_job_verified", "UPDATE", `OLD.error_class='uncertainty' AND NEW.error_class<>'uncertainty'`, merge(jobBase, map[string]string{"event_type": `'uncertainty.resolved'`, "kind": `'observation'`})); err != nil {
		return err
	}
	if err := install("jobs", "activity_job_new_intent", "UPDATE", `OLD.intent_id<>NEW.intent_id`, merge(jobBase, map[string]string{"event_type": `'execution.queued'`, "kind": `'lifecycle'`})); err != nil {
		return err
	}
	relation := merge(jobBase, map[string]string{"operation_id": "OLD.intent_id", "event_type": `CASE WHEN OLD.desired_generation=NEW.desired_generation AND OLD.action=NEW.action AND OLD.force_nonce=NEW.force_nonce THEN 'execution.linked' ELSE 'intent.superseded' END`, "kind": `'relation'`, "data_json": `json_object('jobId',NEW.id,'relatedOperationId',NEW.intent_id,'previousRevisionId',OLD.desired_revision_id,'desiredRevisionId',NEW.desired_revision_id,'phase',CASE WHEN OLD.desired_generation=NEW.desired_generation AND OLD.action=NEW.action AND OLD.force_nonce=NEW.force_nonce THEN 'waiting' ELSE 'ended' END,'result',CASE WHEN OLD.desired_generation=NEW.desired_generation AND OLD.action=NEW.action AND OLD.force_nonce=NEW.force_nonce THEN NULL ELSE 'superseded' END)`})
	if err := install("jobs", "activity_job_intent_relation", "UPDATE", `OLD.intent_id<>NEW.intent_id`, relation); err != nil {
		return err
	}
	// A shared execution serves every equivalent intent, even after the
	// mutable Job points at the newest one. Propagate new facts, never patch
	// the original link or derive historical ownership from the current row.
	if has("jobs") {
		shared := merge(jobBase, map[string]string{"event_type": `'execution.shared_result'`, "operation_id": `linked.operation_id`, "data_json": `json_patch(` + jobData + `,json_object('sharedWithOperationId',NEW.intent_id))`, "source_seq": `(SELECT COALESCE(MAX(source_seq),0) FROM activity_events WHERE source_id='panel-control' AND source_epoch='ledger' AND source_stream_id='control') + row_number() OVER (ORDER BY linked.operation_id)`})
		insert := controlInsert(shared)
		insert = strings.Replace(insert, ") VALUES (", ") SELECT ", 1)
		insert = strings.TrimSuffix(insert, ")")
		insert += ` FROM (SELECT DISTINCT operation_id FROM activity_events e WHERE e.event_type='execution.linked' AND json_extract(e.data_json,'$.jobId')=NEW.id AND e.operation_id<>NEW.intent_id AND NOT EXISTS(SELECT 1 FROM activity_events s WHERE s.operation_id=e.operation_id AND s.event_type='intent.superseded')) linked`
		_, err := db.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS activity_job_shared_result AFTER UPDATE ON jobs WHEN OLD.state<>NEW.state OR OLD.execution_id<>NEW.execution_id OR OLD.error_class<>NEW.error_class BEGIN `+insert+`; END`)
		if err != nil {
			return fmt.Errorf("activity shared execution: %w", err)
		}
	}
	observation := map[string]string{"event_type": `'observation.accepted'`, "kind": `'observation'`, "domain": `'application'`, "operation_id": `COALESCE((SELECT intent_id FROM jobs WHERE id=NEW.last_reconcile_job_id),'')`, "execution_id": `COALESCE((SELECT execution_id FROM jobs WHERE id=NEW.last_reconcile_job_id),'')`, "resources_json": `json_array(json_object('resourceType','application','resourceId',NEW.application_id,'nameSnapshot',COALESCE(` + name("applications", "NEW.application_id") + `,'')),json_object('resourceType','server','resourceId',NEW.server_id,'nameSnapshot',COALESCE(` + name("servers", "NEW.server_id") + `,'')))`, "text": "NEW.observed_state", "data_json": `json_object('instanceId',NEW.id,'source',NEW.observed_source,'observedAt',NEW.observed_at,'observedState',NEW.observed_state,'containerName',NEW.observed_container_name,'containerId',NEW.observed_container_id,'observedGeneration',NEW.observed_generation,'observedSpecHash',NEW.observed_spec_hash,'observedImageDigest',NEW.observed_image_digest,'errorCode',NEW.last_error_code,'error',NEW.last_error_message)`}
	return install("application_instances", "activity_observation", "UPDATE", `NEW.observed_source='reconcile' OR OLD.observed_state<>NEW.observed_state OR OLD.observed_generation<>NEW.observed_generation OR OLD.observed_spec_hash<>NEW.observed_spec_hash OR OLD.observed_container_id<>NEW.observed_container_id OR OLD.last_error_code<>NEW.last_error_code OR OLD.last_error_message<>NEW.last_error_message`, observation)
}

func controlInsert(fields map[string]string) string {
	defaults := map[string]string{"event_id": `'evt_' || lower(hex(randomblob(16)))`, "event_type": `'control.changed'`, "source_id": `'panel-control'`, "source_epoch": `'ledger'`, "source_stream_id": `'control'`, "source_seq": `(SELECT COALESCE(MAX(source_seq),0)+1 FROM activity_events WHERE source_id='panel-control' AND source_epoch='ledger' AND source_stream_id='control')`, "occurred_at": `strftime('%Y-%m-%dT%H:%M:%fZ','now')`, "recorded_at": `strftime('%Y-%m-%dT%H:%M:%fZ','now')`}
	for k, v := range fields {
		defaults[k] = v
	}
	for _, field := range []string{"text", "actor_name"} {
		if expression, ok := defaults[field]; ok {
			defaults[field] = "activity_redact(" + expression + ")"
		}
	}
	for _, field := range []string{"data_json", "resources_json", "initiator_json"} {
		if expression, ok := defaults[field]; ok {
			defaults[field] = "activity_redact_json(" + expression + ")"
		}
	}
	// The control payload hash covers the redacted business fact, independently
	// of the generated event identity and database reception time.
	hashFields := []string{}
	for _, field := range strings.Split(Columns, ",") {
		if field == "event_id" || field == "source_seq" || field == "occurred_at" || field == "recorded_at" {
			continue
		}
		if expression, ok := defaults[field]; ok {
			hashFields = append(hashFields, "'"+field+"'", expression)
		}
	}
	defaults["content_hash"] = "activity_hash(json_object(" + strings.Join(hashFields, ",") + "))"
	columns, values := []string{}, []string{}
	for _, column := range strings.Split(Columns, ",") {
		if value, ok := defaults[column]; ok {
			columns = append(columns, column)
			values = append(values, value)
		}
	}
	return `INSERT INTO activity_events (` + strings.Join(columns, ",") + `) VALUES (` + strings.Join(values, ",") + `)`
}
