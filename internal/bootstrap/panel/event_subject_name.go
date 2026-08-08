package panel

import (
	"context"
	"strings"

	"panel/internal/platform/database"
	"panel/internal/platform/database/orm"
)

// resolveSystemEventSubjectName resolves the display name of an event subject
// at read time. It covers the subject types written by the current event
// producers; unknown or unresolvable subjects return an empty string so the
// frontend can fall back to the raw id.
func resolveSystemEventSubjectName(ctx context.Context, store *database.Store, subjectType, subjectID string) string {
	subjectType = strings.TrimSpace(subjectType)
	subjectID = strings.TrimSpace(subjectID)
	if subjectID == "" {
		return ""
	}
	switch subjectType {
	case "application":
		return subjectNameFromAppDB(ctx, store, "applications", "name", subjectID)
	case "applications":
		return applicationNamesFromIDs(ctx, store, subjectID)
	case "server":
		return subjectNameFromAppDB(ctx, store, "servers", "name", subjectID)
	case "certificate":
		return subjectNameFromAppDB(ctx, store, "certificates", "name", subjectID)
	case "dns_domain":
		return subjectNameFromAppDB(ctx, store, "dns_domains", "name", subjectID)
	case "key_asset":
		return subjectNameFromAppDB(ctx, store, "key_assets", "name", subjectID)
	case "operation":
		return operationNameFromLogDB(ctx, store, subjectID)
	case "task", "task_batch":
		return taskSummaryFromLogDB(ctx, store, subjectID)
	default:
		return ""
	}
}

func subjectNameFromAppDB(ctx context.Context, store *database.Store, table, column, id string) string {
	var name string
	if err := orm.New(store.AppDB()).From(table).Select(column).Where("id = ?", id).ScanValue(ctx, &name); err != nil {
		return ""
	}
	return strings.TrimSpace(name)
}

func applicationNamesFromIDs(ctx context.Context, store *database.Store, ids string) string {
	var parts []string
	for _, part := range strings.Split(ids, ",") {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	type appIDName struct {
		ID   string
		Name string
	}
	var rows []appIDName
	if err := orm.New(store.AppDB()).From("applications").Select("id", "name").AndIn("id", parts).All(ctx, &rows); err != nil {
		return ""
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		if name := strings.TrimSpace(row.Name); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}

func operationNameFromLogDB(ctx context.Context, store *database.Store, operationID string) string {
	var name string
	if err := orm.New(store.LogDB()).From("application_operation_records").Select("application_name_snapshot").Where("operation_id = ?", operationID).ScanValue(ctx, &name); err != nil {
		return ""
	}
	return strings.TrimSpace(name)
}

func taskSummaryFromLogDB(ctx context.Context, store *database.Store, taskID string) string {
	var name string
	if err := orm.New(store.LogDB()).From("tasks").Select("summary").Where("id = ?", taskID).ScanValue(ctx, &name); err != nil {
		return ""
	}
	return strings.TrimSpace(name)
}
