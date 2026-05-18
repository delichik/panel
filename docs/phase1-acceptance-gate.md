# Phase 1 Acceptance Gate

This checklist defines what "Phase 1 complete" means. A feature is not complete if it only renders UI or only exposes an unused API. The service must start, the UI must call real APIs, and operator workflows must close end to end.

## One-Command Startup

Required:

- A documented command starts the panel service from a clean checkout.
- The command initializes required data directories and SQLite databases.
- The backend serves the frontend build or provides a documented development fallback.
- The service prints the listen URL and default login/config source.

Expected final command:

```bash
task run
```

`task run` must install dependencies, build frontend assets, and start the final single backend process that serves both API and frontend.

## Required Closed Loops

### Auth

- Login with configured admin credentials works.
- Invalid credentials fail with `401`.
- Refreshing the browser keeps or restores session state.
- Logout invalidates the session.
- Protected routes and APIs reject unauthenticated access.

### Servers and Credentials

- Operator can create password credentials.
- Operator can create private-key credentials.
- Credential secrets are never echoed back by API or UI.
- Operator can create, edit, list, and delete servers.
- Server can reference a credential.
- Connectivity test creates a task and shows status/logs.
- Debian 12/13 detection is stored and displayed.
- Unsupported distro state is displayed and blocks Debian-specific operations.

### Overview and Metrics

- Overview works with zero servers.
- Overview lists managed servers.
- Selecting a server loads detail charts.
- CPU, memory, disk, and network chart APIs return real series or empty series.
- Metrics are stored in the metrics database, not the application database.
- Metrics retention cleanup can run without damaging app data.

### Package Updates

- Operator can select a server.
- Refresh package list creates or updates package cache.
- Upgradeable package list is displayed.
- Selected-package upgrade creates a task.
- Full upgrade creates a task.
- Unsupported or non-passwordless-sudo servers are blocked with clear errors.
- Task status, stage, and logs are visible during package workflows.

### Task Center

- Task list shows running and recent tasks.
- Task detail shows status, stage, timestamps, optional percentage, and logs.
- Log polling by cursor preserves order.
- Failed tasks include a useful summary and logs.

### Settings

- Runtime settings page loads from real API.
- Secrets are redacted.
- Database paths, data root, metrics retention, and collection interval are visible.

## Verification Commands

Backend:

```bash
task backend:test
task dev
```

Frontend:

```bash
task web:test
task web:build
```

Final integration:

- Start the service with `task run`.
- Open the printed URL.
- Login.
- Complete each closed loop above.

## Test Server Notes

Local test server notes live in `docs/servers.md`. These credentials are for local validation only and must not be copied into public deployment docs.

Expected managed server prerequisites:

- Debian 12 or Debian 13.
- SSH reachable from the panel host.
- Password or private-key SSH login.
- Passwordless sudo for privileged operations.

## Non-Negotiable Rules

- No hardcoded demo data in production paths.
- No mock-only UI as a completion claim.
- No secrets in API responses, frontend state dumps, or task logs.
- No long-running remote operation outside the shared task model.
- No package, metrics, or distro behavior bypassing `DistroAdapter`.
- No raw SSH usage outside `internal/sshx`.
- No OS-specific startup/build/test scripts as the primary workflow; use `Taskfile.yml`.
