# Deployment

Phase 1 starts with one cross-platform command from the repository root:

```bash
task run
```

The project uses [Task](https://taskfile.dev/) for build, test, and run commands on Windows and Linux. `task run` installs frontend dependencies, builds `web/dist`, downloads Go dependencies, and starts the Go backend. The backend serves APIs and the frontend bundle from one process.

Default development login:

- Username: `admin`
- Password: `admin`

For a custom config, copy `config.example.json`, change the session secret and password hash, then start with:

```bash
PANEL_CONFIG=config.local.json task run
```

On Windows PowerShell:

```powershell
$env:PANEL_CONFIG="config.local.json"; task run
```

Useful task commands:

```bash
task deps
task test
task build
task run
task dev
```

`task dev` starts the backend without rebuilding frontend assets. Use `task run` for acceptance checks.

The backend serves APIs under `/api/v1`. If `web/dist` exists, the same process serves the frontend bundle. If `web/dist` does not exist, the root path returns a plain backend health message while the API remains usable.

Runtime defaults:

- App database: `data/db/app.db`
- Metrics database: `data/db/metrics.db`
- SSH private keys: `data/keys`
- Listen address: `127.0.0.1:8080`

Runtime settings managed in the UI and stored in the app database:

- Metrics retention: 7 days
- Metrics collection interval: 60 seconds
- Cleanup schedule: daily

Important production steps:

- Replace `sessionSecret` with a long random value.
- Replace `adminPasswordHash` with a bcrypt hash for the desired admin password.
- Restrict filesystem permissions on the `data` directory.
- Run the process on a trusted control host that can reach managed servers over SSH.
