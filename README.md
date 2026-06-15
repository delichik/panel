# Panel

English | [简体中文](README.zh-CN.md)

Panel is an alpha-stage server operations panel for small Linux fleets. It helps you connect Debian and Ubuntu servers over SSH, see their health at a glance, run package maintenance, deploy container applications through panel-agent and Docker Engine API, and manage DNS and ACME certificates from one web UI.

The project is built for people who run their own servers and for humans sharing code with each other. It aims to stay understandable: a Go backend, a Vue frontend, local SQLite data, and task commands that are easy to run.

## What Panel Can Do

- Add SSH credentials and register servers.
- Probe server reachability, OS details, system traits, and passwordless sudo support.
- Collect overview metrics for CPU, memory, disk, network, uptime, kernel, and load.
- Refresh APT package updates and run selected or full upgrades.
- Install UFW where supported.
- Deploy panel-agent to servers with mTLS and a configured Docker host.
- Check agent compatibility, Docker health, and runtime status.
- Deploy Docker-based applications through panel-agent using Docker Engine API.
- Configure application files, variables, mounts, ports, placement, runtime actions, logs, and reverse proxy routes.
- Manage Cloudflare domains and issue ACME certificates.
- Track background tasks and task logs.
- Switch the UI between English and Simplified Chinese.

## Status

Panel is currently alpha software. It already has useful workflows, but you should expect changes in configuration, database migrations, and UI behavior as the project grows. Use it first on development or non-critical servers, and keep backups of the `data` directory if you run it for real work.

## Supported Target Systems

Panel support is intentionally explicit and will expand over time.

Current supported systems:

- Debian 12
- Debian 13
- Ubuntu 20.04 LTS
- Ubuntu 22.04 LTS
- Ubuntu 24.04 LTS
- Ubuntu 24.10
- Ubuntu 25.04
- Ubuntu 25.10
- Ubuntu 26.04 LTS

Notes:

- Servers are managed over SSH.
- Password or private-key SSH credentials are supported.
- Many maintenance actions require root or passwordless sudo.
- Package maintenance uses APT.
- Application runtime requires panel-agent on target servers and a reachable Docker Engine endpoint. The default Docker host is `unix:///var/run/docker.sock`.

## Quick Start

### Requirements

Install these on the machine where you develop or run Panel:

- Go 1.25+
- Node.js 22+
- npm
- [Task](https://taskfile.dev/)

Docker is optional for containerized deployment.

### 1. Install Web Dependencies

```bash
npm --prefix web ci
```

If you are working without a lockfile-aware flow, `npm --prefix web install` also works.

### 2. Create a Config File

```bash
cp config.example.json config.json
```

Then point Panel at it:

```bash
export PANEL_CONFIG=./config.json
```

PowerShell:

```powershell
$env:PANEL_CONFIG = ".\config.json"
```

Default local login:

- Username: `admin`
- Password: `admin`

Panel requires a password change on first use and rotates the JWT signing secret automatically when the password is changed.

### 3. Start the Backend

```bash
task run:backend
```

The backend listens on `127.0.0.1:8080` by default.

### 4. Start the Web UI

Open another terminal:

```bash
task run:web
```

Open `http://127.0.0.1:5173`. During development, Vite proxies `/api` requests to the backend.

## Configuration

Panel loads configuration in this order:

1. Built-in defaults.
2. The JSON file pointed to by `PANEL_CONFIG`.
3. Environment variables.

Common config values:

| Key | Purpose | Default |
| --- | --- | --- |
| `listenAddress` | Backend listen address | `127.0.0.1:8080` |
| `dataRoot` | Root directory for Panel data | `data` |
| `appDatabase` | Main SQLite database | `data/db/app.db` |
| `metricsDatabase` | Metrics SQLite database | `data/db/metrics.db` |
| `certificates.acmeDirectoryUrl` | ACME directory URL | Let's Encrypt production |

Administrator username/password, JWT secret, remote command timeout, certificate email, and certificate DNS propagation delay are stored in the application database and configured from **Settings** in the UI.

Supported environment variables:

- `PANEL_CONFIG`
- `PANEL_LISTEN_ADDRESS`
- `PANEL_DATA_ROOT`
- `PANEL_APP_DATABASE`
- `PANEL_METRICS_DATABASE`
- `PANEL_CERT_ACME_DIRECTORY_URL`

Runtime settings such as language, token expiration, metrics retention, security settings, and certificate defaults can be adjusted from the UI.

Development-only web proxy variable:

- `PANEL_WEB_PROXY_TARGET`

## Docker

Build the image:

```bash
docker build -t panel .
```

Run it:

```bash
docker run --rm -p 8080:8080 -v panel-data:/app/data panel
```

Container defaults:

- Listens on `0.0.0.0:8080`.
- Stores data in `/app/data`.
- Serves the built web UI from the backend.

For persistent deployments, mount or back up the data volume. Security settings, including the JWT secret, are stored there.

## Development

Useful commands:

```bash
task run:backend
task run:web
task test:backend
task test:web
task build:backend
task build:web
task build
```

Project layout:

```text
cmd/panel              backend entry point
internal/              backend services, handlers, storage, integrations
web/                   Vue 3 frontend
web/src/i18n/          frontend translations
docs/agents/           repository collaboration and i18n notes
tmp/                   temporary test and build files
Taskfile.yml           common task entry points
config.example.json    example runtime config
Dockerfile             production container build
```

Useful entry points:

- Backend startup: `cmd/panel/main.go`
- Route wiring and static UI serving: `internal/app/app.go`
- Database migrations: `internal/storage/migrations.go`
- Target OS adapters: `internal/linux/`
- Agent runtime and Docker API logic: `internal/agent/`, `internal/appruntime/`
- Application deployment logic: `internal/applications/`
- Frontend routes: `web/src/router/index.ts`
- Frontend i18n setup: `web/src/i18n/index.ts`

## Working With Text

Panel has English and Simplified Chinese UI strings. When changing user-visible text in the application, follow the i18n guide in `docs/agents/i18n-guide.md` and keep `docs/agents/i18n-translation-status.md` up to date.

## License

No license file is currently included. If you plan to reuse or redistribute the project, check with the project owner first.
