# Operations

## Authentication

The panel uses one local admin account. The password is verified against the bcrypt hash loaded from config. Sessions are stored in memory and protected by a signed HTTP-only cookie. Restarting the backend invalidates active sessions.

## Server Checks

Connectivity tests run as tasks. The task performs:

1. SSH connection and authentication.
2. `/etc/os-release` detection.
3. `sudo -n true` passwordless sudo check.
4. Server state update with reachability, distro support, and sudo status.

Task logs redact obvious secret key/value patterns.

## Metrics

Metrics are stored in `data/db/metrics.db`, separate from application state. The scheduler collects metrics from reachable, supported servers at the runtime interval through visible `metrics_collect` tasks in Task Center. Empty metric ranges return empty series.

Metrics retention, collection interval, and cleanup schedule are stored in the application database and can be changed from Settings without restarting the process. Retention cleanup deletes rows older than the active retention period.

## Package Updates

Package refresh and upgrade operations are task-backed.

- Refresh uses the Debian adapter and records update cache in the app database.
- Selected upgrades require at least one package name.
- Full upgrades require supported Debian and passwordless sudo.
- Unsupported servers and servers without passwordless sudo are blocked before task creation for mutating upgrade operations.

Package commands are non-interactive and run through `RemoteExecutor` plus `DistroAdapter`; feature services do not embed raw SSH or apt command execution.

## Troubleshooting

- `401` means the session is missing or expired.
- `422 server_not_supported` means `/etc/os-release` is not Debian 12 or Debian 13.
- `422 passwordless_sudo_required` means `sudo -n true` did not pass during server test.
- `502 ssh_connection_failed` means the panel host cannot open SSH to the server.
- `502 ssh_auth_failed` means SSH authentication failed.
