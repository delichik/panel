# Phase 2 Docker Compose Design (Actual & Refined)

This document specifies the actual architecture, implementation logic, and refined design of the Docker Compose-backed service management in the control panel. 

The panel targets an **ultra-lightweight container orchestration tool** using an SSH-only control model to manage Debian servers without installing agents or exposing remote Docker Sockets.

---

## 1. Core Architecture and Implementation Logic

The Docker Compose orchestration layer is designed around a modular monolith divided into two main domains: **Runtime Discovery (`internal/docker`)** and **State Orchestration (`internal/compose`)**.

```
                   +------------------+
                   |     Web UI       |
                   +--------+---------+
                            | (REST API / Tasks)
                            v
            +---------------+---------------+
            |        Backend Monolith       |
            +---------------+---------------+
                            |
           +----------------+----------------+
           |                                 |
           v                                 v
+------------------------+      +------------------------+
|    internal/docker     |      |    internal/compose    |
|  (Runtime Discovery)   |      |  (State & Templates)   |
+-----------+------------+      +-----------+------------+
            |                                |
            |      (SSH / RemoteExecutor)     |
            v                                v
+--------------------------------------------------------+
|                    Remote Debian Server                |
|           (Docker Daemon, Docker Compose CLI)          |
+--------------------------------------------------------+
```

### 1.1 Runtime Discovery (`internal/docker`)
Responsible for reading the remote ground truth of Docker components. It acts as an observer and runs only read-only commands through SSH, caching results in SQLite to prevent network overhead during routine UI polling.

- **Capabilities Check (`CLIRuntime.Detect`)**: Discovers if the target server supports Docker Compose. It executes SSH checks for executable binaries and verifies if the SSH user has sufficient permission to reach the Docker daemon without interactive `sudo` prompts.
- **Resource Listing**: Directly calls CLI tools with JSON/JSON-like formatters to fetch:
  - Docker Compose Projects: `docker compose ls --all --format json`
  - Container Services: `docker ps -a --format '{{json .}}'`
  - Networks: `docker network ls --format '{{json .}}'`
  - Volumes: `docker volume ls --format '{{json .}}'`
  - Images: `docker image ls --digests --format '{{json .}}'`
- **Status Caching**: Discovered capability and resource summaries are written to the SQLite DB cache with timestamps to optimize display latency.

### 1.2 State Orchestration (`internal/compose`)
Manages templates, variables, static files, rendering, deployment workflows, drift calculation, and task integration.

- **Service Templates (`ServiceTemplate`)**: Defines a blueprint for Compose services. Contains:
  - `ComposeYAML`: The raw Compose definition.
  - `Variables`: A list of dynamic configuration items.
  - `VisualState`: Graph or form editor position/layout metadata.
  - `Dependencies`: Required linked template IDs.
- **Template Files (`TemplateFile`)**: Auxiliary files attached to templates. Supports:
  - Static binaries (uploaded as-is).
  - Go text templates (`FileKindTemplate`), rendered with variables dynamically.
- **Deployed Services (`DeployedService`)**: Instances mapped to a specific server. Stores target server ID, base remote path, custom configuration values, drift indicator, and deployment task history.
- **Drift Logic**: When a `ServiceTemplate` is updated, the template version increments, and all associated `DeployedService` instances are marked as `drifted = 1` in the database.

---

## 2. Current Implementation Directory and File Mapping

The following lists the actual code structures built during Phase 2, moving away from original multi-file specifications in favor of cohesive modules:

### 2.1 Backend Modules

#### `internal/docker/`
- `model.go`: Defines structures for `DockerCapability`, `RuntimeService`, `RuntimeNetwork`, `RuntimeVolume`, `RuntimeImage`, and container lists.
- `runtime.go`: Contains `CLIRuntime`, implementing the actual remote parser commands over SSH and parsing command outputs to Go objects.
- `service.go`: Controls capability refresh, task creation for background updates, capability writing, and database cached state.
- `handler.go`: Exposes REST endpoints (`/api/v1/servers/{serverId}/docker/...`) for capabilities, services, networks, volumes, and images.

#### `internal/compose/`
- `model.go`: Defines GDTOs/DTOs for templates (`ServiceTemplate`, `TemplateVariable`), attached files (`TemplateFile`, `SaveFileRequest`), deployed services (`DeployedService`), render actions (`RenderRequest`, `RenderResult`), and validations.
- `service.go`: Encapsulates template CRUD, template-file storage on disk under `data/service_templates`, Go text template rendering, dynamic folder creation, dependencies resolution (`applyDependencies`), and remote deployment task invocation (`LifecycleTask`).
- `handler.go`: Handles API groups for `/api/v1/service-templates` and `/api/v1/services` (including render, deploy, sync, restart, stop, remove, update-images).

---

## 3. Phase 2 Version 1: Key Issues, Constraints, and Technical Debt

As of Version 1, several gaps and design mismatches have been identified that prevent the container management panel from achieving a polished, production-grade lightweight orchestration experience:

### 3.1 Template Configuration Mismatch (SQL vs. YAML)
- **Problem**: In early versions, the service template system tried to persist individual config variables in SQL fields, creating high friction when adapting to varying complex Compose configurations.
- **Resolution**: Make the raw **YAML Compose definition** the single source of truth. All configurable values should be parsed from and serialized to this YAML. The backend must focus on syntax, format, and variable validation, while the frontend dynamically renders form fields (dropdowns, list fields) based on parsing the YAML itself.

### 3.2 Limited Variable Substitution and Context Lack
- **Problem**: Variables were replaced only in specific fields, failing to support flexible options such as volume mounts on the host, container port numbers, or dynamic file paths.
- **Resolution**: Implement dynamic evaluation of variables in **all manually configurable locations**, including:
  1. Volume path bindings.
  2. Template file paths and text template content.
- **Built-in Variables**: Support system-level context values during Go template rendering:
  - `.server`: Current server metadata (`ip`, `username`, `name`, custom variables).
  - `.servers`: A list of all registered servers (useful for cluster or multi-node configurations).
  - `.files`: Paths and content pointers of attached template files.

### 3.3 UI Layout Constraints & Overlap
- **Problem 1 (Two-Column Layout)**: The Service Template detail/editor drawer is split into two wide columns, stretching the screen and creating bad visual alignment.
- **Problem 2 (Duplicate Services Views)**: Services lists are rendered in two places: as part of "Runtime Resources" (directly from Docker daemon) and as panel-managed "Services". This causes navigation confusion.
- **Problem 3 (Scroll Behavior)**: When content overflows, the entire page scrolls, pulling the main navbar and sidebar out of view.
- **Problem 4 (Empty Overview State)**: When no servers are added yet, the Overview page looks blank and confusing to new users, lacking a clear onboard path.

---

## 4. Refined Specifications and Design Polishing Goals

To address the Version 1 issues and complete the lightweight orchestration tool, the design is polished according to the following directives:

### 4.1 UI Polish (Visual & Information Hierarchy)
- **Drawer Narrowing**: Compress the service template creation and editing panel into a single-column, clear layout.
- **Navigation Priority**: Keep **Services** as the first primary item in the Docker sidebar (representing deployed applications) and **Service Templates** as the last item.
- **Deduping Services**: Eliminate container lists from the "Runtime Resources" tab. "Runtime Resources" now exclusively manages Docker infrastructure nodes (Networks, Volumes, Images).
- **Template Badges**: Deployed containers in the "Services" list must clearly display a badge identifying their associated template version and ID. Containers discovered directly on the remote server that do not match panel metadata must be marked as `unmanaged`.
- **Card-Level Scroll**: Ensure all layouts use a fixed full-screen viewport. If a panel card's content overflows, it must scroll internally instead of triggering a body-level scroll bar.

### 4.2 Template Validation Flow & Preview
- **Preview Position**: Reposition the target server selection for preview directly above the generated configuration file to make the testing workflow intuitive.
- **Inline Error Feedback**: Rather than showing validator rows, render validation syntax or rendering errors directly under the corresponding input controls in red, maintaining layout stability.

### 4.3 Task Visibility & Global Awareness
- **Task Header Feed**: The main top bar panel-header will display the current active tasks. If multiple tasks are executing in the background, they must automatically rotate/scroll upward, ensuring continuous system feedback.

---

## 5. Verification Checklist

To confirm a successful implementation of these polished design directives, the following operations must pass:
1. Creating a `service_template` using variable placeholders in both its `composeYaml` and dynamic file paths (e.g. `{{.server.customVar}}`).
2. Selecting a server to preview the rendered YAML immediately, seeing failures inline if variables are missing.
3. Deploying the service, which creates a background task visible in the top-bar task feed.
4. Marking the service as drifted when the template YAML changes, enabling the Operator to trigger a "Sync" operation.
5. Viewing the list of services where template-deployed containers show badges and other containers show `unmanaged`.
