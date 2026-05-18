# Linux Server Panel Design

## 1. Overview

This project is a centralized Linux server management panel deployed on a single control host. The panel exposes a web UI and manages multiple remote Debian servers over SSH only.

The implementation will prioritize:

- A modern web interface
- Centralized multi-server management
- SSH-only remote operations
- Minimal panel authentication
- Explicit extension points for future Docker, DNS, and SSL modules
- Clear separation between configuration data and time-series metrics data

The panel will initially support only:

- Debian 12
- Debian 13
- Password-based SSH login
- SSH private key login
- Passwordless sudo only
- SQLite as the only database backend

Future support for more Linux distributions, database backends, DNS providers, and certificate providers must be enabled through interfaces and module boundaries rather than rewrites.

## 2. Product Scope

### 2.1 Phase 1: MVP

Phase 1 will deliver a complete usable control panel with these capabilities:

- Panel login using a single username/password pair
- Server management
- SSH credential management
- SSH key management
- Debian 12/13 detection
- Overview dashboard
- System metrics charts for CPU, memory, disk, and network
- System status display
- Package update detection
- Single-server package update page
- Partial package upgrades
- Full package upgrades
- Task center with status, current stage, and live logs
- Dual-database support using separate application and metrics databases
- Configurable metrics retention and cleanup

### 2.2 Phase 2: Docker Compose Management

Phase 2 will add:

- Docker Compose project management
- Container/service status views
- Common container configuration page
- Static resource management for container projects
- Dynamic resource management using Go templates
- One-click migration for projects and attached resources

### 2.3 Phase 3: DNS Management

Phase 3 will add:

- DNS provider abstraction
- CRUD operations for DNS records
- Initial Cloudflare provider support

### 2.4 Phase 4: SSL Certificate Management

Phase 4 will add:

- Certificate provider abstraction
- Let's Encrypt support
- Automatic renewal
- Synchronization to managed servers
- Certificate references usable by container projects

## 3. Non-Goals for Phase 1

These items are explicitly out of scope for Phase 1:

- RBAC or multi-user permissions
- Support for non-Debian distributions
- Interactive sudo password prompts
- Docker Engine API integration
- Full Docker project management UI
- DNS management UI
- Certificate issuance UI
- True percentage progress for all long-running tasks

## 4. Core Constraints

### 4.1 Control Model

The panel is centralized:

- One panel instance runs on one control host
- The panel initiates SSH connections to managed servers
- All monitoring, updates, deployments, certificate sync, and later Docker operations originate from the panel

### 4.2 Authentication Model

The panel uses the simplest possible local authentication:

- Single username/password login
- No RBAC
- No per-user permission model
- No SSO or external identity provider

Panel credentials are stored in configuration rather than in the application database.

### 4.3 SSH and Privilege Model

Remote command execution is constrained to:

- SSH password authentication
- SSH private key authentication
- Passwordless sudo only

The panel must not support interactive sudo password entry. If a managed server requires a sudo password, the operation is considered unsupported until the server is configured for passwordless sudo.

### 4.4 Distribution Support

The panel must be designed for multiple distributions later, but only implement:

- Debian 12
- Debian 13

Detection must use `/etc/os-release`. Unsupported distributions are recorded as unsupported without attempting Debian-specific operations.

## 5. System Architecture

The recommended architecture is a modular monolith:

- One Go backend service
- One Vue 3 frontend bundle served by the backend
- One application database
- One metrics database
- One local filesystem data root

This keeps deployment simple while preserving clear module boundaries and extension points.

### 5.1 Backend Stack

- Go
- HTTP framework: Gin or Echo
- ORM or query layer: GORM preferred for initial delivery speed
- SSH: `golang.org/x/crypto/ssh`
- Scheduler: internal periodic job runner
- Application DB: SQLite
- Metrics DB: SQLite

### 5.2 Frontend Stack

- Vue 3
- Vite
- Element Plus
- Pinia
- Vue Router
- ECharts

## 6. Storage Design

Storage is deliberately split into three layers.

### 6.1 Configuration Files

Configuration files store panel-local configuration:

- Listen address and port
- Admin username
- Admin password hash
- Session secret
- Application database backend configuration
- Metrics database backend configuration
- Metrics retention settings
- Cleanup schedule settings
- Data root paths
- Logging configuration

Configuration files do not store business state such as servers or task history.

### 6.2 Application Database

The application database stores structured business data:

- Servers
- Credential metadata
- SSH key metadata
- System package update cache
- Docker update cache data model reserved for future use
- Task history
- DNS record cache
- Certificate metadata
- Future Docker project metadata

Recommended default file:

- `data/db/app.db`

### 6.3 Metrics Database

The metrics database stores time-series snapshots and related retention-managed data:

- CPU snapshots
- Memory snapshots
- Disk snapshots
- Network snapshots
- Basic system status snapshots if useful for chart correlation

Recommended default file:

- `data/db/metrics.db`

The metrics database must support periodic cleanup based on configurable retention.

### 6.4 Filesystem Data Root

The local filesystem stores non-tabular artifacts:

- SSH private keys
- Static container resources
- Dynamic template source files
- Rendered template outputs
- Certificate files
- Downloaded or staged deployment artifacts

Recommended structure:

- `data/keys/`
- `data/compose/<server-id>/<project>/static/`
- `data/compose/<server-id>/<project>/templates/`
- `data/compose/<server-id>/<project>/rendered/`
- `data/certs/`
- `data/tmp/`

## 7. Application Modules

### 7.1 Auth Module

Responsibilities:

- Login endpoint
- Session creation and validation
- Logout
- Password hash verification

Deliberate limits:

- No user CRUD
- No roles
- No external auth

### 7.2 Server Module

Responsibilities:

- CRUD for managed servers
- Server labels or grouping metadata reserved for future use
- Connectivity test
- Distribution detection
- Sudo policy flag
- Default credential association

### 7.3 Credential Module

Responsibilities:

- Store credential metadata
- Support password credentials
- Support SSH private key credentials
- Associate credentials with servers

Sensitive values may be encrypted in the app database or stored on disk with only metadata in the database, but the interface must hide that distinction from other modules.

### 7.4 SSH Module

Responsibilities:

- Open SSH sessions
- Execute commands
- Upload files
- Download files
- Apply timeouts
- Capture stdout/stderr
- Return exit code
- Wrap commands in passwordless sudo when required

This module is the only place where raw SSH behavior should live.

### 7.5 Linux Distribution Adapter Module

Responsibilities:

- Detect supported distributions
- Provide distro-specific command implementations
- Hide package manager and system command differences from higher layers

Phase 1 implements only a Debian adapter.

### 7.6 Metrics Module

Responsibilities:

- Collect CPU, memory, disk, network, load, and uptime data
- Store metrics snapshots in the metrics database
- Aggregate data for chart APIs
- Clean up expired snapshots

### 7.7 Packages Module

Responsibilities:

- Refresh upgradeable package list
- Return update summaries for overview pages
- Execute partial package upgrades
- Execute full package upgrades
- Record task history and logs

### 7.8 Task Module

Responsibilities:

- Persist long-running task metadata
- Persist task logs
- Expose status and current stage APIs
- Support polling-based progress reporting in Phase 1

### 7.9 Docker Module Placeholder

Responsibilities in future phases:

- Manage Compose projects over SSH
- Read container/project status
- Trigger pull, up, restart, recreate operations
- Manage project metadata and migration bundles

### 7.10 DNS Module Placeholder

Responsibilities in future phases:

- Expose DNS record CRUD through provider abstractions

### 7.11 Certificate Module Placeholder

Responsibilities in future phases:

- Issue certificates
- Renew certificates
- Sync certificate files to servers
- Manage certificate references used by projects

## 8. Key Interfaces

The system must define interfaces early so later phases do not force refactoring across module boundaries.

### 8.1 RemoteExecutor

Purpose:

- Unified remote execution and transfer interface

Expected capabilities:

- Connect using password or private key
- Execute command
- Execute command with passwordless sudo
- Upload file
- Download file
- Stream or capture output
- Enforce timeout

### 8.2 DistroAdapter

Purpose:

- Abstract Linux distribution behavior

Expected capabilities:

- Detect support
- Collect metrics
- Read system status
- List upgradable packages
- Upgrade selected packages
- Upgrade all packages

### 8.3 TaskRunner

Purpose:

- Standardize long-running task state transitions

Expected capabilities:

- Create task
- Advance stage
- Append log
- Mark success
- Mark failure

### 8.4 ContainerRuntime

Future purpose:

- Decouple Compose management from Docker implementation details

Phase 2 initial implementation will use:

- SSH + `docker` CLI
- SSH + `docker compose` CLI

The design intentionally does not require Docker Engine API access.

### 8.5 DNSProvider

Future purpose:

- Abstract provider-specific DNS record operations

Initial provider:

- Cloudflare

### 8.6 CertificateProvider

Future purpose:

- Abstract certificate issuance and renewal behavior

Initial provider:

- Let's Encrypt

### 8.7 TemplateRenderer

Future purpose:

- Render dynamic text resources from Go templates before deployment

## 9. Metrics and Retention Design

Metrics are intentionally stored separately from main application state.

### 9.1 Why a Separate Metrics Database

Rationale:

- Metrics are high-volume and retention-based
- Metrics cleanup should not affect main business data
- Future backend replacement is easier if metrics are isolated
- Backup and restore behavior differs from application state

### 9.2 Collection Policy

Initial default:

- Collect every 60 seconds

Data shown in UI:

- Last 1 hour
- Last 6 hours
- Last 24 hours

### 9.3 Retention Policy

Retention must be configurable through the panel configuration file.

Examples:

- Keep raw metrics for 7 days
- Keep raw metrics for 30 days

Phase 1 requires deletion of expired data. Downsampling is not required in Phase 1.

### 9.4 Cleanup Execution

The scheduler runs periodic cleanup jobs against the metrics database. Cleanup results should be logged through the task or scheduler logging system.

## 10. Remote Data Collection

The panel is not intended to be a full observability platform. Metrics collection should stay lightweight.

### 10.1 Required Overview Data

Per server:

- CPU usage
- Memory total/used/available
- Disk total/used/available
- Network receive/transmit rates
- Load average
- Uptime
- Hostname
- Kernel version
- OS version
- Current server time
- Package update count
- Docker update count field reserved for future Docker update checks

### 10.2 Collection Method

Collection runs over SSH using distro-specific commands. Phase 1 should favor commands that are available on standard Debian 12/13 hosts without requiring extra agents.

## 11. Package Update Design

### 11.1 Update Detection

The scheduler periodically refreshes package update information for each server and caches the result in the application database.

The overview page displays:

- Whether updates are available
- Number of upgradable packages
- Last refresh timestamp

### 11.2 Package Update Page

The package update page is single-server oriented but supports server switching.

Required capabilities:

- Select a server
- Refresh package list manually
- Show upgradeable package list
- Partial upgrade for selected packages
- Full upgrade for all packages
- Show task status and live logs for running operations

Displayed fields should include:

- Package name
- Installed version
- Candidate version
- Repository/source if available

### 11.3 Progress and Status Model

The UI does not require a true numeric percentage for every operation.

Required behavior:

- Always show current stage
- Always show current status
- Always show live or near-live logs
- Show numeric percentage only when the command output makes it reliably available

This avoids false precision while still providing useful operator feedback.

## 12. Task State Model

All long-running operations should use one shared task model.

### 12.1 Core Status Values

- `queued`
- `running`
- `completed`
- `failed`
- `cancelled` reserved for future cancellation support

### 12.2 Core Stage Values

Suggested common stages:

- `connecting`
- `preparing`
- `running`
- `verifying`
- `finalizing`

Modules may define more specific stage names if needed, but the model should remain consistent across package updates, future deployments, migrations, and renewals.

### 12.3 Task UI Requirements

The frontend must show:

- Current task status
- Current task stage
- Start time
- End time if finished
- Live log stream or polling log view
- Optional percentage if supported by the underlying command

Phase 1 may use polling rather than WebSocket streaming to reduce implementation complexity.

## 13. Frontend Information Architecture

Phase 1 pages:

- Login
- Overview
- Servers
- Package Updates
- Task Center
- Settings

### 13.1 Login Page

Responsibilities:

- Username/password login
- Minimal and clean visual presentation

### 13.2 Overview Page

Responsibilities:

- Show all managed servers as summary cards
- Show health and update indicators
- Allow selecting one server for detail charts
- Render CPU, memory, disk, and network charts
- Show system metadata and last collection time

### 13.3 Servers Page

Responsibilities:

- List servers
- Add/edit/delete servers
- Manage credentials
- Manage SSH keys
- Test connectivity
- Show detected distro support state

### 13.4 Package Updates Page

Responsibilities:

- Switch current server
- Display update list
- Refresh update cache
- Execute selected updates
- Execute all updates
- Observe task status and logs

### 13.5 Task Center

Responsibilities:

- Show recent and running tasks
- Open task detail view
- View current stage and logs

### 13.6 Settings

Responsibilities:

- Show basic panel information
- Optionally surface read-only runtime info in Phase 1

## 14. Docker Design Direction

This section defines future boundaries only; it is not a Phase 1 implementation target.

### 14.1 Runtime Approach

Initial runtime approach:

- SSH into the server
- Use `docker` CLI
- Use `docker compose` CLI

This is preferred over Docker Engine API because:

- It aligns with the SSH-only control model
- It avoids exposing Docker API on managed servers
- It reduces deployment and security complexity

### 14.2 Progress Reporting

For future Docker tasks:

- Show stage and logs always
- Show percentages only when CLI output provides stable parseable progress

### 14.3 Compose Resource Model

A Compose project owns:

- Compose configuration
- Static resources
- Dynamic template resources
- Rendered outputs
- Optional linked certificates

### 14.4 Static vs Dynamic Resources

Static resources:

- Binaries
- Directories
- Arbitrary files copied as-is

Dynamic resources:

- Text files only
- Generated by Go templates using project variables
- Written into the project-specific rendered directory before deployment

### 14.5 Migration Direction

One-click migration will package:

- Project definition
- Static resources
- Template sources
- Rendered output or render inputs
- Relevant metadata
- Certificate references

## 15. DNS Design Direction

The DNS module will expose provider-neutral CRUD operations.

Initial provider:

- Cloudflare

Required record capabilities:

- List
- Create
- Update
- Delete

No advanced synchronization behavior is required in the initial DNS implementation.

## 16. SSL Design Direction

The certificate module will expose provider-neutral issuance and renewal flows.

Initial provider:

- Let's Encrypt

Required future capabilities:

- Create certificate request
- Renew certificate
- Sync cert/key files to servers
- Associate certificates with projects or servers

Container projects should reference deployed certificate locations rather than depend directly on provider-specific logic.

## 17. API Design Principles

The backend API should follow these principles:

- REST-first for Phase 1
- Clear separation between command operations and query operations
- Stable resource identifiers
- Explicit task creation for long-running actions

Examples of long-running actions:

- Refresh package cache
- Upgrade selected packages
- Upgrade all packages
- Test server connectivity if implemented asynchronously

## 18. Security Considerations

Phase 1 security goals are pragmatic rather than enterprise-grade.

Required controls:

- Store panel password as a hash
- Protect sessions with a strong secret
- Isolate SSH private keys under a dedicated local directory
- Avoid logging secrets
- Restrict privilege execution to passwordless sudo paths only

Future enhancements may include:

- Secret encryption at rest
- Audit logs
- IP allowlists

## 19. Documentation Requirements

The project must keep documentation aligned with implementation.

Required documents:

- This design document
- A Phase 1 implementation plan
- Deployment and configuration guide
- Operations guide for managed server prerequisites

Managed server prerequisites must clearly state:

- Debian 12 or 13 only
- SSH reachable from the panel host
- Password or SSH key login available
- Passwordless sudo required for privileged operations
- Docker CLI and Compose availability for future Docker features

## 20. Recommended Initial Directory Layout

Recommended project layout:

- `cmd/panel/`
- `internal/auth/`
- `internal/server/`
- `internal/credential/`
- `internal/sshx/`
- `internal/linux/`
- `internal/metrics/`
- `internal/packages/`
- `internal/tasks/`
- `internal/scheduler/`
- `internal/storage/`
- `internal/config/`
- `web/`
- `data/`
- `docs/superpowers/specs/`
- `docs/superpowers/plans/`

This layout preserves modularity while keeping the project simple to navigate.

## 21. Recommended Delivery Decision

Proceed with a Phase 1 implementation that builds a working, deployable management panel for:

- Centralized Debian 12/13 server access
- Overview monitoring
- System package update management
- SSH key and credential management
- Task status and log tracking

At the same time, define and preserve the following long-term interfaces from day one:

- `RemoteExecutor`
- `DistroAdapter`
- `TaskRunner`
- `ContainerRuntime`
- `DNSProvider`
- `CertificateProvider`
- `TemplateRenderer`

This gives the project a practical MVP without sacrificing future Docker, DNS, SSL, or multi-backend expansion.
