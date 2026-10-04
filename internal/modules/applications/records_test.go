package applications

import (
	"context"
	"errors"
	"testing"
	"time"

	panelerr "panel/internal/platform/errors"
)

func insertOperationRecordApplication(t *testing.T, svc *Service, ctx context.Context, id string, kind string, createdAt time.Time) {
	t.Helper()
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO applications(id,version,kind,name,enabled,deletion_requested,spec_yaml,deployment_mode,deployment_server_ids_json,generation,spec_hash,image_reference,job_id,namespace,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, 1, kind, id, 1, 0,
		"name: "+id+"\nimage: nginx:alpine\n", DeploymentModeAll, `["srv-a"]`,
		1, "hash-"+id, "nginx:alpine", id, "default", formatTime(createdAt), formatTime(createdAt)); err != nil {
		t.Fatalf("insert application %s: %v", id, err)
	}
}

func insertOperationRecordJob(t *testing.T, svc *Service, ctx context.Context, appID string, intentID string, createdAt time.Time) {
	t.Helper()
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO jobs(id,application_id,server_id,instance_id,action,state,intent_id,trigger_type,trigger_resource_type,trigger_resource_id,reason,desired_spec_json,last_steps_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"job-"+appID, appID, "srv-a", "instance-"+appID, "apply", "pending", intentID,
		"manual", "application", appID, "test", "{}", "[]", formatTime(createdAt), formatTime(createdAt)); err != nil {
		t.Fatalf("insert job %s: %v", appID, err)
	}
}

func TestOperationRecordsExcludeHiddenFacilityApplications(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)

	insertOperationRecordApplication(t, svc, ctx, "facility-hidden-kind", ApplicationKindFacility, base)
	insertOperationRecordApplication(t, svc, ctx, FacilityProxyApplicationID, ApplicationKindUser, base.Add(time.Second))
	insertOperationRecordApplication(t, svc, ctx, "user-app", ApplicationKindUser, base.Add(2*time.Second))
	insertOperationRecordJob(t, svc, ctx, "facility-hidden-kind", "intent-facility-kind", base)
	insertOperationRecordJob(t, svc, ctx, FacilityProxyApplicationID, "intent-facility-identity", base.Add(time.Second))
	insertOperationRecordJob(t, svc, ctx, "user-app", "intent-user-app", base.Add(2*time.Second))

	page, err := svc.ListApplicationOperationRecords(ctx, OperationRecordFilter{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("operation page = %#v, want one visible record", page)
	}
	if page.Items[0].ApplicationID != "user-app" || page.Items[0].OperationID != "intent-user-app" {
		t.Fatalf("visible record = %#v, want normal user application", page.Items[0])
	}

	for _, operationID := range []string{"intent-facility-kind", "intent-facility-identity"} {
		detail, err := svc.GetApplicationOperationRecord(ctx, operationID)
		var perr *panelerr.Error
		if err == nil || !errors.As(err, &perr) || perr.Code != "not_found" || perr.HTTPStatus != 404 {
			t.Fatalf("detail for %s err = %v, want application_operation not found (detail=%#v)", operationID, err, detail)
		}
	}

	filtered, err := svc.ListApplicationOperationRecords(ctx, OperationRecordFilter{
		ApplicationID: FacilityProxyApplicationID,
		Limit:         50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 0 || len(filtered.Items) != 0 {
		t.Fatalf("filtered facility page = %#v, want empty", filtered)
	}
}

func TestOperationRecordsTolerateLegacyEmptyTimeColumns(t *testing.T) {
	svc, _, _, closeStore := newTestService(t)
	defer closeStore()
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)

	insertOperationRecordApplication(t, svc, ctx, "legacy-app", ApplicationKindUser, base)
	// 模拟旧版本 orchestrator 把 jobs 可空时间列写成空串 ''（而非 NULL）的
	// 存量数据；记录列表/详情必须能正常扫描，不能报 orm: cannot parse time。
	if _, err := svc.db.ExecContext(ctx, `INSERT INTO jobs(id,application_id,server_id,instance_id,action,state,intent_id,trigger_type,trigger_resource_type,trigger_resource_id,reason,desired_spec_json,last_steps_json,next_run_at,lease_expires_at,started_at,finished_at,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"legacy-job", "legacy-app", "srv-a", "inst-legacy", "apply", "pending", "intent-legacy",
		"manual", "application", "legacy-app", "test", "{}", "[]", "", "", "", "", formatTime(base), formatTime(base)); err != nil {
		t.Fatalf("insert legacy job with '' time columns: %v", err)
	}

	page, err := svc.ListApplicationOperationRecords(ctx, OperationRecordFilter{Limit: 50})
	if err != nil {
		t.Fatalf("list records with legacy '' time columns: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatalf("operation page = %#v, want one legacy record", page)
	}
	if page.Items[0].OperationID != "intent-legacy" {
		t.Fatalf("visible record = %#v, want intent-legacy", page.Items[0])
	}
	detail, err := svc.GetApplicationOperationRecord(ctx, "intent-legacy")
	if err != nil {
		t.Fatalf("detail with legacy '' time columns: %v", err)
	}
	if len(detail.Targets) != 1 {
		t.Fatalf("detail targets = %d, want 1", len(detail.Targets))
	}
}
