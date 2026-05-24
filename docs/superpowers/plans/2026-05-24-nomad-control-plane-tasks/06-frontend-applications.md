# Task 06: Frontend Applications Workspace

**Goal:** Replace the Container Services UI with a Nomad-backed Applications workspace.

**Architecture:** Vue pages call `/api/v1/applications`. The first screen is the operational application table and detail view, not a landing page. The editor uses YAML as the source of truth and surfaces validate, plan, deploy, stop, restart, runtime, and logs actions.

**Tech Stack:** Vue 3, Vite, Vuetify, Pinia, existing frontend API client conventions.

---

## Ownership

Primary owner: Frontend application workspace.

Can start with API mocks after Task 04 publishes response examples. Full integration requires Task 04 merged.

Blocks:

- Task 08 final verification.

## Backend API Dependencies

From Task 04:

- `GET /api/v1/applications`
- `POST /api/v1/applications`
- `PUT /api/v1/applications/{id}`
- `POST /api/v1/applications/{id}/validate`
- `POST /api/v1/applications/{id}/plan`
- `POST /api/v1/applications/{id}/deploy`
- `POST /api/v1/applications/{id}/stop`
- `POST /api/v1/applications/{id}/restart`
- `GET /api/v1/applications/{id}/runtime`
- `GET /api/v1/applications/{id}/logs`

## Related Interface Docs

- Shared frontend DTOs: `00-coordination.md`
- Backend Application service/API task: `04-application-service-api.md`
- Nomad job/deployment/allocation source APIs used by backend runtime responses: `docs/nomad-api-docs/jobs.mdx`, `docs/nomad-api-docs/deployments.mdx`, `docs/nomad-api-docs/evaluations.mdx`, `docs/nomad-api-docs/allocations.mdx`
- Nomad JSON job fields shown in plan/preview UI: `docs/nomad-api-docs/json-jobs.mdx`

## Files

Create:

- `web/src/api/applications.ts`
- `web/src/api/applications.test.ts`
- `web/src/features/applications/pages/ApplicationsPage.vue`
- `web/src/features/applications/components/ApplicationEditor.vue`
- `web/src/features/applications/components/ApplicationDetail.vue`
- `web/src/features/applications/components/ApplicationRuntimePanel.vue`
- `web/src/features/applications/components/ApplicationLogsPanel.vue`
- `web/src/features/applications/ApplicationsPage.test.ts`

Modify:

- `web/src/types/api.ts`
- `web/src/router/index.ts`
- `web/src/layouts/AppLayout.vue`

Delete after replacement:

- `web/src/features/container-services/`
- `web/src/api/containerServices.ts`
- `web/src/api/containerServices.test.ts`

## UI Contract

Applications page:

- top actions: refresh, create;
- summary strip: total apps, enabled apps, unhealthy/pending apps;
- table columns: name, enabled, runtime status, job ID, namespace, generation, last eval, actions;
- detail pane: deployment status, allocations, evaluations, logs, latest error.

Editor:

- create mode: name, enabled switch, YAML spec, variables map;
- edit mode: immutable name display, enabled switch, YAML spec, variables map;
- actions: validate, plan, save, save and deploy;
- validation issues shown with path, severity, message.

Actions:

- deploy -> `POST /deploy`;
- stop -> `POST /stop`;
- restart -> `POST /restart`;
- logs -> `GET /logs`.

## Steps

- [ ] **Step 1: Write failing API client tests**

Create `web/src/api/applications.test.ts` covering `list`, `create`, `update`, `validate`, `plan`, `deploy`, `stop`, `restart`, `runtime`, and `logs`.

- [ ] **Step 2: Verify failure**

Run:

```bash
cd web
npm test -- applications
```

Expected: FAIL because API client does not exist.

- [ ] **Step 3: Implement API client and DTOs**

Add `ApplicationDto`, `ApplicationSaveDto`, `ApplicationRuntimeDto`, `ApplicationValidationDto`, `ApplicationPlanDto`, `ApplicationOperationDto`, and Nomad nested DTOs to `web/src/types/api.ts`. Implement `applicationsApi` in `web/src/api/applications.ts`.

- [ ] **Step 4: Build page layout**

Create `ApplicationsPage.vue` using the current operational layout style from `ContainerServicesPage.vue`, but replace Compose-specific copy with Nomad/Application language.

- [ ] **Step 5: Build editor**

Create `ApplicationEditor.vue` with YAML textarea/editor, variables section, validation result panel, and plan preview panel. Keep UI controls compact and operational.

- [ ] **Step 6: Build detail panels**

Create runtime and logs panels. Allocations should show allocation ID, node ID, task group, client status, desired status, task states, and recent event message.

- [ ] **Step 7: Update routes and navigation**

Add `/applications`. Redirect `/container-services` to `/applications`. Remove old Container Services navigation entry and add Applications.

- [ ] **Step 8: Run verification**

Run:

```bash
cd web
npm test -- applications
npm run build
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add web/src
git rm -r web/src/features/container-services web/src/api/containerServices.ts web/src/api/containerServices.test.ts
git commit -m "feat: add applications workspace"
```

## Handoff Notes

Tell Task 08 owners whether any old `container-services` references remain for intentional redirects only.
