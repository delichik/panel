# Phase 1 Verification Checklist

Use Task for cross-platform verification on Windows and Linux.

```bash
task test
task build
task run
```

Start command:

```bash
task run
```

API smoke checks:

- Open `http://127.0.0.1:8080/`.
- Log in with the configured admin account.
- Confirm `GET /api/v1/auth/session` returns the authenticated username.
- Confirm `GET /api/v1/settings/runtime` returns paths and runtime settings without secrets.
- Confirm `PUT /api/v1/settings/runtime` updates metrics retention, collection interval, and cleanup schedule without restarting.

Server and credential checks:

- Create a password credential for the Debian test server user.
- Create a server using a host from `docs/servers.md`.
- Run connectivity test.
- Poll `GET /api/v1/tasks/{taskId}` until completed or failed.
- Poll `GET /api/v1/tasks/{taskId}/logs?after=0` and confirm no password or private key appears.
- Confirm the server shows Debian 12 or Debian 13 support state after a successful test.

Metrics checks:

- Wait one metrics collection interval after a successful server check.
- Query `GET /api/v1/servers/{serverId}/metrics?range=1h`.
- Confirm CPU, memory, disk, and network arrays are returned.

Package checks:

- Run package refresh.
- Confirm a task is created with `202`.
- Poll logs until completion.
- Confirm `GET /api/v1/servers/{serverId}/packages/updates` returns the cached list.
- Run selected upgrade against a safe test package if available.
- Run full upgrade only on disposable test servers.

Storage checks:

- Confirm `data/db/app.db` and `data/db/metrics.db` both exist.
- Confirm package/task/server data is in the app DB.
- Confirm metric snapshots are in the metrics DB.

Automated checks:

```bash
task test
task build
```
