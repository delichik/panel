# Seamark

English | [简体中文](README.zh-CN.md)

Seamark is an alpha-stage server operations panel for small Linux fleets. It helps you connect Debian and Ubuntu servers over SSH, see their health at a glance, run package maintenance, deploy container applications through panel-agent and Docker Engine API, and manage DNS and ACME certificates from one web UI.

The project is built for people who run their own servers and for humans sharing code with each other. It aims to stay understandable: a Go backend, a Vue frontend, local SQLite data, and task commands that are easy to run.

## What Seamark Can Do

- Add SSH credentials and register servers.
- Probe server reachability, OS details, system traits, and passwordless sudo support.
- Collect overview metrics for CPU, memory, disk, network, uptime, kernel, and load.
- Refresh APT package updates and run selected or full upgrades.
- Install UFW where supported.
- Deploy panel-agent to servers with mTLS and a configured Docker host.
- Check agent compatibility, Docker health, and runtime status.
- Deploy Docker-based applications through panel-agent using Docker Engine API.
- Configure application files, mounts, ports, placement, runtime actions, logs, and reverse proxy routes.
- Manage Cloudflare domains and issue ACME certificates.
- Track background tasks and task logs.
- Switch the UI between English and Simplified Chinese.

## Status

Seamark is currently alpha software. It already has useful workflows, but you should expect changes in configuration, database migrations, and UI behavior as the project grows. Use it first on development or non-critical servers, and keep backups of the Seamark data volume if you run it for real work.

## Supported Target Systems

Seamark support is intentionally explicit and will expand over time.

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

## Get Started

Seamark deployment is container-only for end users:

- [Deploy Seamark with Docker Compose or Docker](docs/deployment.md)
- [Follow the first-use and application deployment guide](docs/user-guide.md)

The deployment guide covers persistent storage, first login, HTTPS, backup, upgrades, and troubleshooting. The user guide walks through credentials, servers, panel-agent, Docker health, the first application, domains, certificates, tasks, and daily maintenance.

## Development

The commands below are for working on Seamark from source. They are not required for a normal deployment.

### Requirements

- Go 1.25+
- Node.js 22+
- npm
- [Task](https://taskfile.dev/)

Docker is only required when building or testing the production container image.

### Run From Source

Install web dependencies:

```bash
npm --prefix web ci
```

Copy the example configuration:

```bash
cp config.example.json config.json
export PANEL_CONFIG=./config.json
```

PowerShell:

```powershell
Copy-Item config.example.json config.json
$env:PANEL_CONFIG = ".\config.json"
```

Start the backend:

```bash
task run:backend
```

Open another terminal and start the web development server:

```bash
task run:web
```

Open `http://127.0.0.1:5173`. Vite proxies `/api` requests to the backend.

The local development login is `admin/admin`. Seamark requires a password change on first use.

### Development Configuration

Seamark loads configuration in this order:

1. Built-in defaults.
2. The JSON file pointed to by `PANEL_CONFIG`.
3. Environment variables.

Common config values:

| Key | Purpose | Default |
| --- | --- | --- |
| `listenAddress` | Backend listen address | `127.0.0.1:8080` |
| `dataRoot` | Root directory for Seamark data | `data` |
| `appDatabase` | Main SQLite database | `data/db/app.db` |
| `metricsDatabase` | Metrics SQLite database | `data/db/metrics.db` |
| `certificates.acmeDirectoryUrl` | ACME directory URL | Let's Encrypt production |

Supported environment variables:

- `PANEL_CONFIG`
- `PANEL_LISTEN_ADDRESS`
- `PANEL_DATA_ROOT`
- `PANEL_APP_DATABASE`
- `PANEL_METRICS_DATABASE`
- `PANEL_CERT_ACME_DIRECTORY_URL`
- `PANEL_WEB_PROXY_TARGET` for the development web proxy

Administrator credentials, the JWT secret, command timeout, certificate email, language, token expiration, metrics retention, security settings, and certificate defaults are stored in the application database and managed from **Settings**.

### Commands

```bash
task run:backend
task run:web
task test:backend
task test:web
task build:backend
task build:web
task build
```

### Project Layout

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
- Route wiring and static UI serving: `internal/bootstrap/panel/app.go`
- Database migrations: `internal/platform/database/migrations.go`
- Target OS adapters: `internal/platform/linux/`
- Agent runtime and Docker API logic: `internal/agent/`, `internal/modules/applications/runtime/`
- Application deployment logic: `internal/modules/applications/`
- Frontend routes: `web/src/router/index.ts`
- Frontend i18n setup: `web/src/i18n/index.ts`

## Working With Text

Seamark has English and Simplified Chinese UI strings. When changing user-visible text in the application, follow the i18n guide in `docs/agents/i18n-guide.md` and keep `docs/agents/i18n-translation-status.md` up to date.

## License

No license file is currently included. If you plan to reuse or redistribute the project, check with the project owner first.
