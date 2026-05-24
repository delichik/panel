# Task 07: Frontend Nomad Nodes And Deployments Views

**Goal:** Add operational views for Nomad nodes, jobs, deployments, evaluations, and services.

**Architecture:** These views are read-only dashboards backed by `/api/v1/nomad/*`. They replace Docker Runtime Explorer as the primary runtime observation surface.

**Tech Stack:** Vue 3, Vite, Vuetify, existing API client conventions.

---

## Ownership

Primary owner: Frontend operations views.

Can start with mocked API after Task 05 publishes sample JSON. Full integration requires Task 05 merged.

Blocks:

- Task 08 final verification.

## Backend API Dependencies

- `GET /api/v1/nomad/status`
- `GET /api/v1/nomad/nodes`
- `GET /api/v1/nomad/jobs`
- `GET /api/v1/nomad/deployments`
- `GET /api/v1/nomad/evaluations`
- `GET /api/v1/nomad/services`

## Related Interface Docs

- Shared frontend DTOs: `00-coordination.md`
- Backend Nomad inventory task: `05-nomad-runtime-inventory.md`
- Nomad nodes API: `docs/nomad-api-docs/nodes.mdx`
- Nomad jobs API: `docs/nomad-api-docs/jobs.mdx`
- Nomad deployments API: `docs/nomad-api-docs/deployments.mdx`
- Nomad evaluations API: `docs/nomad-api-docs/evaluations.mdx`
- Nomad services API: `docs/nomad-api-docs/services.mdx`

## Files

Create:

- `web/src/api/nomad.ts`
- `web/src/api/nomad.test.ts`
- `web/src/features/nomad/pages/NomadNodesPage.vue`
- `web/src/features/nomad/pages/NomadJobsPage.vue`
- `web/src/features/deployments/pages/DeploymentsPage.vue`

Modify:

- `web/src/types/api.ts`
- `web/src/router/index.ts`
- `web/src/layouts/AppLayout.vue`

## UI Contract

Nomad Nodes:

- connection status banner;
- node table with name, ID, datacenter, status, scheduling eligibility;
- node status color: ready -> success, down -> error, other -> warning;
- allocation count if API provides it.

Nomad Jobs:

- job ID, name, type, status, namespace, datacenters;
- filter by prefix in UI state only for first version.

Deployments:

- deployment ID, job ID, status, status description;
- evaluations table: ID, job ID, type, status;
- services table: service name, tags, namespace.

## Steps

- [ ] **Step 1: Write failing API tests**

Create `web/src/api/nomad.test.ts` covering all six `/api/v1/nomad/*` methods.

- [ ] **Step 2: Verify failure**

Run:

```bash
cd web
npm test -- nomad
```

Expected: FAIL because API client does not exist.

- [ ] **Step 3: Implement API client and DTOs**

Add DTOs:

- `NomadStatusDto`
- `NomadNodeDto`
- `NomadJobDto`
- `NomadDeploymentDto`
- `NomadEvaluationDto`
- `NomadServiceRegistrationDto`

Implement `nomadApi` with `status`, `nodes`, `jobs`, `deployments`, `evaluations`, and `services`.

- [ ] **Step 4: Implement pages**

Create pages listed in Files. Use dense operational tables and avoid marketing copy.

- [ ] **Step 5: Update routes and navigation**

Add routes:

```ts
{ path: 'nomad/nodes', name: 'nomad-nodes', component: NomadNodesPage, meta: { title: 'Nomad Nodes' } }
{ path: 'nomad/jobs', name: 'nomad-jobs', component: NomadJobsPage, meta: { title: 'Nomad Jobs' } }
{ path: 'deployments', name: 'deployments', component: DeploymentsPage, meta: { title: 'Deployments' } }
```

Redirect `/runtime-explorer` to `/nomad/nodes`.

- [ ] **Step 6: Run verification**

Run:

```bash
cd web
npm test -- nomad deployments
npm run build
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src
git commit -m "feat: add nomad operations views"
```

## Handoff Notes

Tell Task 08 owners whether any old Runtime Explorer references remain for intentional redirects only.
