# Task 03: Application Persistence Schema

**Goal:** Replace fresh-schema Container Service persistence with Application persistence for Nomad-backed desired state and revisions.

**Architecture:** SQLite remains the application database. Fresh development schema can remove old Compose orchestration tables because the project is in early development and compatibility is not required.

**Tech Stack:** Go, SQLite, existing `internal/storage` migration style.

---

## Ownership

Primary owner: Backend storage.

Can start immediately.

Blocks:

- Task 04 Application service.
- Task 08 cleanup verification.

## Related Existing Files

- `internal/storage/migrations.go`
- `internal/storage/store_test.go`
- `internal/containerservice/model.go`
- `internal/containerops/worker.go`

## Related Interface Docs

- Shared Application DTO: `00-coordination.md`
- Application API contract: `00-coordination.md`
- Application service task: `04-application-service-api.md`
- Nomad rendered job shape that will be stored in `application_revisions.job_json`: `docs/nomad-api-docs/json-jobs.mdx`

## Tables To Add

### `applications`

```sql
CREATE TABLE IF NOT EXISTS applications (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 0,
	spec_yaml TEXT NOT NULL,
	variables_json TEXT NOT NULL DEFAULT '{}',
	generation INTEGER NOT NULL DEFAULT 1,
	spec_hash TEXT NOT NULL DEFAULT '',
	job_id TEXT NOT NULL,
	namespace TEXT NOT NULL DEFAULT 'default',
	last_eval_id TEXT NOT NULL DEFAULT '',
	last_deployment_id TEXT NOT NULL DEFAULT '',
	last_error TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(name)
)
```

### `application_files`

```sql
CREATE TABLE IF NOT EXISTS application_files (
	id TEXT PRIMARY KEY,
	application_id TEXT NOT NULL,
	path TEXT NOT NULL,
	kind TEXT NOT NULL CHECK(kind IN ('binary','template')),
	content_type TEXT NOT NULL DEFAULT '',
	size INTEGER NOT NULL DEFAULT 0,
	sha256 TEXT NOT NULL DEFAULT '',
	content BLOB,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(application_id, path),
	FOREIGN KEY(application_id) REFERENCES applications(id) ON DELETE CASCADE
)
```

### `application_revisions`

```sql
CREATE TABLE IF NOT EXISTS application_revisions (
	id TEXT PRIMARY KEY,
	application_id TEXT NOT NULL,
	generation INTEGER NOT NULL,
	spec_hash TEXT NOT NULL,
	spec_yaml TEXT NOT NULL,
	job_json TEXT NOT NULL,
	created_at TEXT NOT NULL,
	UNIQUE(application_id, generation),
	FOREIGN KEY(application_id) REFERENCES applications(id) ON DELETE CASCADE
)
```

## Tables To Remove From Fresh Schema

- `docker_capabilities`
- `docker_runtime_cache`
- `container_runtime_cache`
- `operation_locks`
- `container_services`
- `container_service_files`
- `container_service_placements`

Keep `tasks`, `task_steps`, and `task_logs`.

## Steps

- [ ] **Step 1: Write failing migration test**

Add a test in `internal/storage/store_test.go` that migrates a fresh DB and asserts:

- `applications` exists.
- `application_files` exists.
- `application_revisions` exists.
- old tables listed above do not exist.

- [ ] **Step 2: Verify failure**

Run: `go test ./internal/storage`

Expected: FAIL because Application tables are missing and old tables still exist.

- [ ] **Step 3: Update migrations**

Modify `internal/storage/migrations.go`:

- add the three Application tables;
- remove old orchestration tables from fresh schema;
- keep task tables unchanged;
- keep settings table unchanged.

- [ ] **Step 4: Add Application model shell**

Create `internal/applications/model.go` with `Application`, task type constants, `ApplicationFile`, `ApplicationRevision`, `Runtime`, `SaveInput`, `OperationResult`, and validation DTOs. Do not implement service behavior here; Task 04 owns service behavior.

- [ ] **Step 5: Run verification**

Run:

```bash
go test ./internal/storage
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/storage/migrations.go internal/storage/store_test.go internal/applications/model.go
git commit -m "feat: add application persistence schema"
```

## Handoff Notes

Tell Task 04 owners the exact column names and model field names. Task 04 should not rename columns without coordinating with frontend DTO owners.
