# Phase 2 Acceptance Gate

This checklist defines what "Phase 2 complete" means. A feature is not complete if it only adds backend scaffolding, a static UI, or a one-off Docker command. Docker service templates, deployed services, resource cleanup, image updates, and Compose-backed workflows must close through real APIs, task logs, filesystem artifacts, labels, and remote server state.

## Prerequisites

Required test coverage should include at least two managed servers:

- One Debian 12 or Debian 13 server with Docker Engine and Docker Compose plugin installed.
- One managed server without Docker, or with Docker intentionally unavailable in `PATH`.

Optional but recommended:

- A second Docker-capable server for migration import validation.
- A test `service_template` with at least two Compose services.
- A binary template-attached file.
- A text template file with one required variable.
- At least one server custom variable configured on a Docker-capable server.

## Phase 2A Required Closed Loops

### Docker Capability

- Operator can select a server and refresh Docker capability.
- Servers without Docker show an unsupported state, not a generic error.
- Servers with Docker show Docker version and Compose version.
- Capability responses include checked time and last error when applicable.
- Refresh failure does not delete the last known successful capability cache.

### Runtime Docker Status

- Operator can list runtime services/containers for a Docker-capable server.
- Runtime service list is read from the remote server, not panel-owned metadata.
- Operator can open a runtime service and see service/container status.
- Operator can list Docker networks, volumes, and images.
- Empty lists, loading states, unsupported states, and command failures are visible in the UI.
- Runtime status refresh does not require creating a panel-owned project.

### Networks, Volumes, and Images

- Operator can view Docker networks.
- Operator can delete a selected network through a task-backed workflow.
- Operator can delete unused networks through a task-backed prune workflow.
- Operator can view Docker volumes.
- Operator can delete a selected volume through a task-backed workflow.
- Operator can delete unused volumes through a task-backed prune workflow.
- Operator can view Docker images.
- Operator can delete a selected image through a task-backed workflow.
- Operator can delete unused images through a task-backed prune workflow.
- In-use delete failures are shown as clear Docker resource-in-use errors.

### Image Updates

- Operator can check image updates for a selected server.
- Available image updates are displayed similarly to system package updates.
- Operator can select specific image updates and run a task-backed update.
- Operator can run update all for all available image updates.
- Image update tasks show pull/recreate/status-refresh stages and logs.
- Failed image updates do not mark the service as updated.

### Task and Log Visibility

- Long-running Docker refreshes create or update operator-visible task/log state when they cannot complete inline.
- Failed refreshes include enough command context to diagnose the issue without leaking secrets.

## Phase 2B Required Closed Loops

### Service Template CRUD

- Operator can create, edit, list, and delete `service_template` metadata.
- Template names are validated for path safety and Compose compatibility.
- Template editing supports both visual mode and YAML mode.
- Visual mode and YAML mode use the same validation/render/deploy pipeline.
- Deleting template metadata does not silently remove remote containers, volumes, images, or files.

### Services

- Operator can create a deployed `service` from a `service_template`.
- Operator can list deployed services and inspect linked template, server, status, drift state, and last task.
- Service names and remote base paths are validated before deployment.
- Operator can start, stop, restart, remove, and sync a service through task-backed workflows.
- Removing a service is explicit about containers, networks, volumes, images, and remote files.

### Template-Attached Files

- Operator can add, update, list, and delete binary/static files attached to a template.
- Binary/static files are stored under `data/service_templates/<template-id>/static/`.
- Binary/static files deploy unchanged.
- Operator can add, update, list, and delete text template files attached to a template.
- Text template files are stored under `data/service_templates/<template-id>/templates/`.
- Rendered outputs are stored under `data/compose/<server-id>/<service>/rendered/`.
- Template validation errors are shown before deployment and block remote mutation.
- Render preview uses the same renderer and values as deployment.

### Variables

- System variables are available to every render.
- Server custom variables can be configured per managed server.
- Template files can reference system variables, server custom variables, and service-specific values.
- Missing variables produce hard validation errors with the variable name and file context.
- Missing variables never render as empty strings by default.
- Server custom variables are not treated as secrets unless a later secret-specific model is added.

### Template Sync

- Every deployed service created by the panel has Docker labels linking it to template ID, template version, service ID, and server ID.
- Runtime discovery uses labels to associate containers/resources with panel metadata.
- Editing a template creates a new template version.
- Linked services are marked as drifted after a template change.
- Operator can sync selected linked services through task-backed workflows.
- Sync only targets services linked by metadata and labels.
- Unmanaged Docker resources without matching labels are displayed but not mutated by template sync.

### Deployment

- Deploy creates a task and returns `202 Accepted` with `taskId`.
- Task logs show validation, rendering, staging upload, activation, Compose pull, Compose up, and post-deploy status refresh.
- Remote upload uses a staging path before activation.
- Compose config and resources land in predictable remote paths.
- A failed validation or render step performs no remote mutation.
- A failed upload or Compose command leaves task logs with cleanup or retry guidance.
- Service detail shows last deploy task, linked template version, drift state, update prompts, and current runtime status.

### Operations

- Pull, image update, restart, stop, remove, template sync, migration export, and migration import are task-backed.
- Restart can target selected services when the API supports service selection.
- Stop does not delete metadata or local resources.
- Remove behavior is explicit about containers, networks, volumes, images, and remote files.

### Migration

- Export bundle contains template metadata, service metadata when exporting a service, binary files, text template files, render values, rendered outputs when needed, labels, certificate references, and a bundle version.
- Import validates bundle version, target server Docker capability, template/service name collision, path safety, variable availability, label compatibility, and resource integrity.
- Import can target a different managed server.
- Import failure does not leave hidden partial metadata.
- Imported service can render and deploy without manual file repair.

## Verification Commands

Backend:

```bash
task backend:test
```

Frontend:

```bash
task web:test
task web:build
```

Final integration:

- Start the service with `task run`.
- Login.
- Complete all 2A closed loops against Docker-capable and unsupported servers.
- Complete all 2B closed loops against a Docker-capable server.
- Export a template or service and import it onto another Docker-capable server.

## Non-Negotiable Rules

- No Docker Engine API dependency in Phase 2.
- No Docker or Compose mutation outside the shared task model.
- No raw SSH usage outside `internal/sshx`.
- No production path using demo Compose data.
- No template deployment without local validation and rendering first.
- No missing template variable may be silently ignored or rendered as an empty value.
- No remote path built from unsanitized template, service, file, or resource names.
- No template sync may mutate Docker resources that lack matching panel labels.
- No API or task log may reveal SSH secrets, registry secrets, environment secrets, certificate private keys, or provider tokens.
- No milestone is complete until backend, frontend, task logs, docs, and acceptance evidence agree.
