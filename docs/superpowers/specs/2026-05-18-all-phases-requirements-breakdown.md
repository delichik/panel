# All Phases Requirements Breakdown

Source design: `docs/superpowers/specs/2026-05-16-linux-server-panel-design.md`

This document decomposes the full Linux server panel roadmap into implementation phases and work packages. It explicitly balances current implementation difficulty against future implementation difficulty: Phase 1 should stay small enough to ship, but it must lay down the contracts that prevent Docker, DNS, and SSL work from forcing rewrites later.

## Delivery Strategy

Recommended order:

1. Phase 1: MVP control panel foundation.
2. Phase 2A: Docker runtime discovery and Compose project read operations.
3. Phase 2B: Compose project deployment, resources, templates, and migration.
4. Phase 3A: DNS provider framework and Cloudflare CRUD.
5. Phase 3B: DNS records usable by later certificate/domain workflows.
6. Phase 4A: Certificate provider framework and Let's Encrypt issuance.
7. Phase 4B: Renewal, server sync, and certificate references for Compose projects.

This splits the original four product phases into smaller engineering milestones. The original phase numbers remain product phases; the A/B split is only for lower-risk implementation.

## Difficulty and Sequencing Principles

- Build reusable task/log infrastructure early because package updates, Docker operations, migration, DNS sync, and certificate renewal all need operator-visible execution state.
- Define interfaces early, but implement only what each phase needs. Interface stubs are cheap; premature workflows are expensive.
- Keep SSH-only remote control as a hard boundary. Docker and certificate sync should use SSH and CLI operations, not a separate remote daemon.
- Put high-risk workflows behind dry-run or validation steps before adding destructive actions.
- Keep provider modules isolated. Cloudflare and Let's Encrypt details must not leak into project, certificate, or DNS screens.
- Store large or file-like artifacts on disk and metadata in the app database. This matters for Compose resources, rendered templates, certificates, and migration bundles.
- Keep the frontend information architecture stable. Add navigation entries by module, not by technical implementation detail.

## Phase 1: MVP Control Panel Foundation

Goal:

- Ship a usable Debian server management panel with auth, server inventory, credentials, metrics, package updates, tasks, and settings.

Core requirements:

- Single local panel login.
- Server CRUD and SSH credential management.
- Debian 12/13 detection through `/etc/os-release`.
- SSH password and private-key login.
- Passwordless sudo only.
- Metrics collection and retention in a separate metrics SQLite database.
- Package update detection, selected upgrades, and full upgrades.
- Shared task center with status, stage, logs, and polling.
- Settings page with redacted runtime information.

Implementation difficulty:

- Moderate. SSH, task logs, package output parsing, and metrics parsing are the riskiest pieces.

Future difficulty reduced by Phase 1:

- `RemoteExecutor`, `DistroAdapter`, `TaskRunner`, scheduler, storage split, and frontend polling patterns become reusable for later phases.

Defer deliberately:

- Docker project CRUD.
- DNS provider credentials.
- Certificate issuance.
- Advanced audit/security features.

Acceptance checks:

- Debian 12 and Debian 13 test servers can be added, tested, monitored, and updated.
- Metrics data lives only in the metrics database.
- Long-running operations produce task logs.
- Unsupported servers are recorded but blocked from Debian operations.

## Phase 2A: Docker Runtime Discovery and Read-Only Compose Views

Goal:

- Introduce Docker support without immediately taking ownership of deployment mutations.

Core requirements:

- Detect Docker CLI and Docker Compose CLI availability per server.
- Read Compose project list over SSH.
- Read container/service status for a selected project.
- Surface Docker availability on overview and server detail screens.
- Store Docker capability/cache data in the application database.

Implementation difficulty:

- Moderate. Output parsing and environment differences are the main risk.

Future difficulty reduced:

- Establishes `ContainerRuntime` and Compose status models before write operations.
- Lets frontend navigation and data models stabilize before resource management is added.

Defer deliberately:

- Creating projects.
- Uploading resources.
- Running `docker compose up`, restart, recreate, or migration operations.

Acceptance checks:

- Servers without Docker are shown as unsupported for Docker operations.
- Servers with Docker show projects and service/container status.
- Docker status refresh uses shared task/log infrastructure when refresh is long-running.

## Phase 2B: Compose Project Management, Resources, Templates, and Migration

Goal:

- Manage Docker Compose projects end to end through SSH and filesystem-backed resources.

Core requirements:

- Compose project CRUD in panel metadata.
- Common container/project configuration page.
- Static resource management under `data/compose/<server-id>/<project>/static/`.
- Dynamic text resource templates under `data/compose/<server-id>/<project>/templates/`.
- Rendered outputs under `data/compose/<server-id>/<project>/rendered/`.
- Go template rendering through `TemplateRenderer`.
- Deploy, pull, up, restart, recreate, stop, and remove workflows.
- One-click migration bundles containing project metadata, static resources, templates, render inputs, rendered outputs when needed, and certificate references.

Implementation difficulty:

- High. File ownership, template correctness, remote path consistency, migration integrity, and destructive Docker actions all require careful task staging and validation.

Future difficulty reduced:

- Resource and project metadata become the anchor for SSL certificate references and future DNS/domain integration.
- Migration format becomes a stable compatibility contract.

Risk controls:

- Validate project configuration before deployment.
- Render templates locally before upload.
- Upload to staging paths before moving into active paths.
- Record every mutating operation as a task.
- Add rollback notes to task logs when automatic rollback is not available.

Acceptance checks:

- A Compose project can be created, deployed, restarted, and inspected.
- Static and dynamic resources deploy to predictable remote paths.
- Template rendering fails before remote deployment if inputs are invalid.
- Migration export and import preserve project metadata and resources.

## Phase 3A: DNS Provider Framework and Cloudflare CRUD

Goal:

- Add provider-neutral DNS management with Cloudflare as the first implementation.

Core requirements:

- DNS provider credential metadata and secret handling.
- Zone list and zone selection.
- DNS record list, create, update, and delete.
- Cloudflare provider implementation behind `DNSProvider`.
- DNS record cache in the application database.
- Task-backed provider sync or refresh where needed.

Implementation difficulty:

- Moderate. Provider API behavior, validation rules, pagination, and rate limits are the main risk.

Future difficulty reduced:

- Provider-neutral record models enable more DNS providers later.
- DNS records can later support certificate validation and Compose project domain hints.

Defer deliberately:

- Automatic reconciliation loops.
- Advanced DNS record types beyond the agreed initial set.
- Multi-provider synchronization.

Acceptance checks:

- Cloudflare zones and records can be listed.
- Supported records can be created, edited, and deleted.
- Provider errors are shown without leaking provider tokens.
- Record cache refresh does not corrupt local metadata if provider calls fail.

## Phase 3B: DNS Integration Points for Projects and Certificates

Goal:

- Make DNS records useful to other modules without coupling those modules to Cloudflare.

Core requirements:

- Link DNS records or hostnames to Compose projects.
- Store domain metadata independent of provider-specific record IDs.
- Expose DNS selection components for future certificate requests.
- Provide validation helpers for hostname, zone, and record ownership.

Implementation difficulty:

- Low to moderate if Phase 3A models are clean; high if Cloudflare details leak into UI/domain models.

Future difficulty reduced:

- Certificate issuance can ask for a domain reference rather than inventing its own domain model.

Acceptance checks:

- A project can reference a hostname/domain.
- Domain references survive provider record cache refreshes.
- Certificate screens can reuse domain selection models without provider-specific fields.

## Phase 4A: Certificate Provider Framework and Let's Encrypt Issuance

Goal:

- Add provider-neutral certificate issuance with Let's Encrypt as the first provider.

Core requirements:

- Certificate metadata in the app database.
- Certificate files under `data/certs/`.
- `CertificateProvider` interface for issue and renew flows.
- Let's Encrypt provider implementation.
- Domain ownership validation workflow.
- Task-backed issuance with logs and clear stages.

Implementation difficulty:

- High. ACME flows, DNS validation timing, file secrecy, and failure recovery are easy to get wrong.

Future difficulty reduced:

- Certificates become reusable resources that Compose projects can reference.
- Renewal uses the same metadata and task model as issuance.

Risk controls:

- Store cert/key material under dedicated filesystem paths.
- Never log private keys, ACME account keys, or provider tokens.
- Use explicit stages for account setup, challenge creation, validation wait, issuance, storage, and verification.
- Keep DNS challenge operations behind DNS provider abstractions when DNS automation is used.

Acceptance checks:

- A certificate can be issued for a managed domain.
- Certificate metadata and files are stored separately.
- Failure leaves enough task logs for diagnosis without exposing secrets.

## Phase 4B: Renewal, Server Sync, and Compose Certificate References

Goal:

- Make certificates operationally useful through renewal, deployment to servers, and Compose project references.

Core requirements:

- Automatic renewal scheduler.
- Renewal task logs and failure states.
- Sync certificate/key files to managed servers over SSH.
- Certificate references on Compose projects.
- Remote certificate path management.
- Deployment workflows that can refresh projects after certificate sync when configured.

Implementation difficulty:

- High. Renewal timing, remote sync permissions, secret handling, and project reload behavior require careful sequencing.

Future difficulty reduced:

- Provides a durable certificate lifecycle that can support more certificate providers later.

Risk controls:

- Renew before expiry with configurable threshold.
- Use staging paths and atomic move where possible.
- Restrict remote file permissions.
- Keep project reload optional and explicit.

Acceptance checks:

- Certificates renew automatically before expiry.
- Sync tasks deploy cert/key files to selected servers.
- Compose projects can reference certificate deployment paths.
- Failed sync or renewal does not delete the last known good certificate.

## Cross-Phase Data Ownership

Application database:

- Servers.
- Credential metadata.
- Task history and logs.
- Package update cache.
- Docker capability cache and Compose project metadata.
- DNS provider metadata, zone cache, record cache, and domain references.
- Certificate metadata and references.

Metrics database:

- Time-series server metrics only.

Filesystem data root:

- SSH private keys.
- Compose static resources, templates, rendered outputs, migration bundles.
- Certificate files and ACME account material.
- Temporary upload and deployment staging artifacts.

## Cross-Phase Team Ownership

Backend Platform:

- Config, storage, migrations, auth, tasks, scheduler, shared HTTP behavior.

Backend Remote:

- Credentials, SSH executor, distro adapters, remote filesystem helpers.

Backend Operations:

- Metrics, packages, Docker runtime, Compose workflows.

Backend Providers:

- DNS providers, certificate providers, provider credentials, provider error handling.

Frontend Shell:

- App bootstrap, router, API client, auth store, layout, navigation, shared task/log components.

Frontend Features:

- Overview, servers, packages, Docker, DNS, certificates, settings.

QA/Docs:

- Deployment docs, managed server prerequisites, provider setup docs, verification checklists, migration test matrix.

## Global Done Criteria

- Each phase ships working software, not only scaffolding.
- Every mutating long-running operation creates a task and logs stages.
- Provider-specific behavior stays behind provider interfaces.
- SSH-only control model is preserved.
- Secrets are not returned by APIs or written to logs.
- Documentation is updated with each phase's prerequisites, configuration, and verification steps.
