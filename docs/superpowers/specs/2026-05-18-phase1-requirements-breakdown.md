# Phase 1 Requirements Breakdown

Source design: `docs/superpowers/specs/2026-05-16-linux-server-panel-design.md`

This document decomposes Phase 1 into independently assignable product slices. Each slice must remain usable through stable module APIs so backend, frontend, and integration work can proceed in parallel.

## Delivery Goal

Build a deployable Linux server panel that manages Debian 12/13 servers from one control host through SSH only. Phase 1 must include login, server and credential management, overview monitoring, package update operations, task tracking, SQLite storage, and metrics retention.

## Scope Rules

In scope:

- Single local panel account with username and password.
- Debian 12 and Debian 13 only.
- SSH password login and private-key login.
- Passwordless sudo only for privileged remote operations.
- SQLite application database and SQLite metrics database.
- Polling-based task and log updates.
- Extension interfaces for future Docker, DNS, SSL, template, and provider work.

Out of scope:

- RBAC, multi-user permissions, SSO, external identity providers.
- Non-Debian implementation.
- Interactive sudo password prompts.
- Docker, DNS, and certificate UI implementation.
- Numeric progress unless the underlying command exposes reliable progress data.

## Requirement Slices

### R1. Panel Authentication

User outcome:

- An operator can log in, keep a valid session, and log out.

Backend requirements:

- Load admin username and password hash from config.
- Verify password hash on login.
- Create, validate, and destroy sessions.
- Protect all non-login APIs.

Frontend requirements:

- Login page with username/password form.
- Redirect unauthenticated users to login.
- Preserve authenticated route access after refresh.

Acceptance checks:

- Correct credentials return an authenticated session.
- Wrong credentials return `401`.
- Protected APIs reject unauthenticated requests.
- Logout invalidates the session.

### R2. Server Inventory

User outcome:

- An operator can register Debian servers, edit metadata, test connectivity, and see whether the distro is supported.

Backend requirements:

- CRUD managed servers.
- Store host, port, SSH username, credential reference, labels, and notes.
- Test SSH connectivity.
- Detect `/etc/os-release`.
- Mark unsupported distributions without running Debian-specific commands.
- Store passwordless sudo capability as an explicit server state.

Frontend requirements:

- Servers page with list, create, edit, delete, and detail views.
- Connectivity test action with visible result.
- Distro support state display.

Acceptance checks:

- Debian 12/13 servers are marked supported.
- Unsupported distros are saved but blocked from package and metrics operations.
- Connectivity failures surface actionable errors without leaking secrets.

### R3. Credential and SSH Key Management

User outcome:

- An operator can manage password credentials and SSH private-key credentials, then associate them with servers.

Backend requirements:

- Store credential metadata in the application database.
- Store or encrypt sensitive material behind a credential store interface.
- Save private keys under the configured data root when file-backed storage is used.
- Hide storage details from server and SSH modules.

Frontend requirements:

- Credential form embedded in server workflows or reachable from the Servers page.
- Credential type selector for password or private key.
- Private key input must not echo back the stored key after save.

Acceptance checks:

- Server connectivity can use either password or private key credential.
- API responses never return credential secrets.
- Deleting a credential that is still in use is rejected with `409`.

### R4. Remote Execution Foundation

User outcome:

- Higher modules can run remote commands, transfer files, and receive structured command results without handling raw SSH details.

Backend requirements:

- Implement `RemoteExecutor`.
- Apply command timeouts.
- Capture stdout, stderr, exit code, and duration.
- Support passwordless sudo command wrapping.
- Keep raw SSH library usage inside the SSH module.

Frontend requirements:

- No direct frontend surface. Frontend observes this through server tests, metrics, packages, and tasks.

Acceptance checks:

- Command success and failure are distinguishable by exit code.
- Timeouts return a typed timeout error.
- Sudo-required commands fail clearly when passwordless sudo is unavailable.

### R5. Distribution Adapter

User outcome:

- Phase 1 Debian behavior is implemented behind an interface that can later support other distributions.

Backend requirements:

- Implement `DistroAdapter` and `DebianAdapter`.
- Detect Debian version from `/etc/os-release`.
- Provide Debian commands for status, metrics, package list, selected upgrade, and full upgrade.
- Return unsupported state for unknown distributions.

Frontend requirements:

- Show distro name, version, support status, and operation eligibility.

Acceptance checks:

- Debian 12 and Debian 13 are accepted.
- Package and metrics modules call adapter methods, not inline command strings.

### R6. Metrics Collection and Retention

User outcome:

- An operator can see recent CPU, memory, disk, and network charts for each server.

Backend requirements:

- Periodically collect metrics over SSH.
- Store metrics in `data/db/metrics.db`.
- Query chart ranges for last 1 hour, 6 hours, and 24 hours.
- Clean expired metrics based on config retention.
- Keep metrics schema separate from application schema.

Frontend requirements:

- Overview charts for CPU, memory, disk, and network.
- Range selector for 1h, 6h, and 24h.
- Last collection timestamp and stale-data state.

Acceptance checks:

- Metrics collection does not write to the application database except optional status cache.
- Cleanup removes expired records and logs the result.
- Empty metrics return an empty series rather than an error.

### R7. Overview Dashboard

User outcome:

- An operator can quickly inspect all servers, identify health/update issues, and inspect one server's charts.

Backend requirements:

- Aggregate server summary, latest status, metrics availability, and update counts.
- Provide server detail metadata for selected server.

Frontend requirements:

- Overview page with server summary cards or table rows.
- Selected server detail area with charts and system metadata.
- Health indicators for connectivity, metrics freshness, and package updates.

Acceptance checks:

- Overview loads with zero servers.
- Overview remains usable if one server fails collection.
- Server selection changes chart data without page reload.

### R8. Package Update Detection and Execution

User outcome:

- An operator can refresh package updates, view upgradeable packages, upgrade selected packages, and upgrade all packages for one server.

Backend requirements:

- Refresh package cache per server.
- Store package update cache in application database.
- Start long-running tasks for refresh and upgrade operations.
- Use `DistroAdapter` for package commands.
- Log command output through `TaskRunner`.

Frontend requirements:

- Package Updates page with server selector.
- Refresh action.
- Upgradeable package table with selected-package upgrade.
- Full upgrade action.
- Running task panel with current stage and logs.

Acceptance checks:

- Refresh updates the package list and timestamp.
- Partial upgrade sends only selected package names.
- Full upgrade runs through the shared task model.
- Unsupported servers cannot start package operations.

### R9. Task Center

User outcome:

- An operator can inspect running and recent long-running operations, including stage and logs.

Backend requirements:

- Persist task metadata and append-only task logs in the application database.
- Support status values: `queued`, `running`, `completed`, `failed`, `cancelled`.
- Support common stages: `connecting`, `preparing`, `running`, `verifying`, `finalizing`.
- Expose polling APIs for task lists, detail, and logs.

Frontend requirements:

- Task Center page listing recent and running tasks.
- Task detail view showing status, stage, timestamps, optional percentage, and logs.
- Package page can embed a task summary without duplicating task logic.

Acceptance checks:

- Task status changes are visible through polling.
- Failed tasks include error summary and logs.
- Logs preserve ordering.

### R10. Settings and Runtime Configuration

User outcome:

- An operator can see basic panel runtime information and knows which settings come from config.

Backend requirements:

- Load config for listen address, credentials, database paths, data root, retention, cleanup schedule, and logging.
- Provide read-only settings summary API with sensitive values redacted.

Frontend requirements:

- Settings page showing runtime information and redacted paths/retention values.

Acceptance checks:

- Secrets are not returned by settings API.
- Missing optional config uses documented defaults.

## Cross-Cutting Requirements

Security:

- Never log SSH passwords, private keys, session secrets, or password hashes.
- Enforce passwordless sudo; do not prompt interactively.
- Private keys must live under the configured data root.

Reliability:

- Remote operations must set timeouts.
- Per-server failures must not block unrelated servers.
- Long-running operations must create tasks before remote execution starts.

Extensibility:

- Future Docker, DNS, certificate, and template modules must compile against interfaces, even when Phase 1 only provides placeholders.
- Debian command logic must stay behind `DistroAdapter`.
- SSH behavior must stay behind `RemoteExecutor`.

## Team Ownership Map

Suggested parallel workstreams:

- Backend Platform: config, storage, migrations, auth, task runner, scheduler.
- Backend Remote: credentials, SSH executor, distro adapter, package workflows.
- Backend Metrics: metrics collection, metrics DB schema, retention, chart queries.
- Frontend App Shell: routing, auth store, API client, layout, shared components.
- Frontend Feature Pages: overview, servers, package updates, task center, settings.
- QA/Docs: managed-server prerequisites, deployment guide, API contract verification, test server validation.

## Dependency Order

1. Config and storage foundations.
2. Auth and frontend app shell.
3. Server, credential, SSH, and distro modules.
4. Task model and scheduler.
5. Metrics collection and overview APIs.
6. Package update workflows.
7. Feature pages and integration polish.
8. Deployment and operations documentation.
