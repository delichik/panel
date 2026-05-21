# Phase 2 Docker Compose Collaboration Plan (Actualized)

This document is the official implementation roadmap for Phase 2 (Docker Compose management) in the panel. It represents the transition from the initial MVP architecture to the actual, optimized implementation and covers the **Version 1 Refinement Milestone (Milestone 2C)**.

---

## Milestone 2A: Docker Capability & Runtime Discovery (Completed)

**Goal:** Establish read-only visibility into remote Docker engines over SSH without performing modifications, using low-overhead cached queries.

### Backend Execution
- **Key Modules**: `internal/docker/runtime.go`, `internal/docker/service.go`, `internal/docker/handler.go`
- **Actions**:
  - Implemented Docker and Compose CLI availability checks over SSH.
  - Implemented caching for discovered Docker capability state per server in SQLite.
  - Implemented parser logic for reading JSON output from remote commands:
    - Compose Projects (`docker compose ls`)
    - Container lists (`docker ps -a`)
    - Networks (`docker network ls`)
    - Volumes (`docker volume ls`)
    - Images (`docker image ls`)
  - Added background task handling to fetch Docker capability on demand.
  - Ensured failed remote scans do not corrupt the last known successful cache.

### Frontend Integration
- **Key Views**: `web/src/features/docker/pages/DockerPage.vue`, `web/src/features/docker/pages/DockerRuntimePage.vue`
- **Actions**:
  - Added Docker status summary indicator cards.
  - Built tab interfaces for listing services, networks, volumes, and images.
  - Rendered error banners and capability checks dynamically.

---

## Milestone 2B: Service Templates & Deployed Services (Completed)

**Goal:** Introduce reusable template blueprints, template-attached binary/text configurations, variables rendering, lifecycle tasks, and drift detection.

### Backend Execution
- **Key Modules**: `internal/compose/model.go`, `internal/compose/service.go`, `internal/compose/handler.go`
- **Actions**:
  - Implemented `ServiceTemplate` CRUD in `service.go`.
  - Implemented `DeployedService` metadata state machine (Draft, Ready, Drifted, Removed).
  - Implemented disk storage for attached binary and template text files under `data/service_templates/`.
  - Built dynamic Go template parsing and evaluation for text-based resources before remote copying.
  - Integrated with the `tasks` service to support background executions for deployment lifecycle hooks (`deploy`, `sync`, `restart`, `stop`, `remove`, `update-images`).
  - Added label-based tracking matching panel metadata to active Docker configurations.
  - Set up service drift detection triggers. If a template is updated, linked services automatically flag as drifted in the DB.

### Frontend Integration
- **Key Views**: `web/src/features/compose/pages/ServiceTemplatesPage.vue`, `web/src/features/compose/pages/ServicesPage.vue`, `web/src/features/compose/components/...`
- **Actions**:
  - Implemented a detailed Compose Template form enabling both visual configuration and direct YAML editing.
  - Created file attachment panel lists to handle binaries and templates.
  - Built a server preview selector allowing users to verify rendered YAML configurations.
  - Provided action controls to deploy, start, restart, and remove active compose stacks.

---

## Milestone 2C: Version 1 Polish & Refinement (Current Active Phase)

**Goal:** Correct Version 1 design constraints, make YAML the unified source of truth, enable variable substitutions everywhere, enforce structured full-screen layouts, and establish cross-context task awareness.

### 2C.1: Unified YAML and Flexible Variables
- [ ] **YAML as Truth**: Deprecate form-binding to strict SQL database columns for Compose properties. Make the raw `composeYaml` the definitive template target. Enable the frontend visual editor to dynamically parse and serialize directly to this YAML.
- [ ] **Arbitrary Variable Support**: Modify the render pipeline in `internal/compose/service.go` to support Go template substitution in:
  - Custom file pathways and mounts (e.g. `data/volumes/{{.server.custom_dir}}`).
  - File contents of type `FileKindTemplate`.
- [ ] **Context Richness**: Inject comprehensive built-ins:
  - `.server`: Current server metadata (IP, username, server name, and key-value custom variables).
  - `.servers`: Accessible array of all registered servers.
  - `.files`: Direct variable map of all attached template files and contents.
- [ ] **Server Variable Editing**: Provide a dedicated field/dialog in `web/src/features/servers/pages/ServersPage.vue` to edit custom variable key-values per server.

### 2C.2: Visual Polish, Scrolling, and Layout Deduping
- [ ] **Single-Column Form Flow**: Refactor the service template creation drawer from a confusing double-column view to a clean, single-column alignment.
- [ ] **Sidebar Priority**: Order Docker operations inside the application shell as:
  1. **Services** (Priority #1)
  2. **Runtime Resources** (Networks, Volumes, Images)
  3. **Service Templates** (Management and definitions)
- [ ] **Resource Isolation**: Remove container list grids from the "Runtime Resources" tab. "Runtime Resources" will exclusively list host networks, volumes, and images to prevent view duplication.
- [ ] **Template Badge Markers**: Display distinctive badges on container grids showing template origins. Directly discovered containers on target engines with no matching panel records must explicitly display `unmanaged` labels.
- [ ] **Inline Preview & Testing**: Move target server selector for YAML rendering directly above the editor window. Remove the redundant validation table row, replacing it with inline validation errors under the specific inputs.
- [ ] **Fixed Viewport Scroll**: Update stylesheet mappings (`web/src/styles/main.css`) to enforce fixed full-screen apps. Cards or content bodies that exceed bounds must handle internal scroll scopes (`overflow-y: auto`) so the primary top-bar and sidebar navigations never scroll off the page.

### 2C.3: Global Task Banner & Onboarding Flows
- [ ] **Onboarding Guides**: Redesign the Overview page when no servers exist to render a welcoming onboarding screen, directing users to register their first Debian host.
- [ ] **Scrolling Task Banner**: Inject a rotating, auto-scrolling active tasks indicator in `web/src/layouts/AppLayout.vue`'s header top-bar. It must pull from `/api/v1/tasks` to cycle through currently executing tasks in real time.

---

## Phase 2 Done Criteria (Refined)

- [x] Docker CLI capability check works for Debian 12 & 13 nodes over SSH.
- [x] Service templates YAML definitions render with local system variables.
- [x] Template version drift triggers correctly when YAML config changes.
- [ ] Variables can be parsed inside template content, YAML fields, and arbitrary folder routes.
- [ ] Managed services show template status badges, and extraneous containers show `unmanaged`.
- [ ] Runtime resources (images, networks, volumes) can be audited and pruned via tasks.
- [ ] Fixed layouts exist everywhere, supporting internal card-scrolling instead of body-scrolling.
- [ ] Global active task indicator rotates running operations in the top bar.
