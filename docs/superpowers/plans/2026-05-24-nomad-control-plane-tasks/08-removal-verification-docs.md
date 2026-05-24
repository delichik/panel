# Task 08: Remove Compose Orchestration And Verify Branch

**Goal:** Remove the old Docker Compose orchestration product surface, document Nomad operation, and verify the branch end to end.

**Architecture:** After Tasks 01-07 land, the active product path is Applications -> Nomad. Old Container Services, Compose include, placement, operation locks, and Docker label idempotency must not remain in active backend or frontend code.

**Tech Stack:** Go, Vue, Vite, ripgrep, existing docs.

---

## Ownership

Primary owner: Integration/cleanup.

Can start only after Tasks 01-07 are merged.

## Files

Delete:

- `internal/containerops/`
- `internal/containerrender/`
- `internal/containerservice/`
- `internal/placement/`
- `web/src/features/container-services/`
- `web/src/api/containerServices.ts`
- `web/src/api/containerServices.test.ts`

Modify:

- `internal/app/app.go`
- `internal/scheduler/scheduler.go`
- `internal/docker/`
- `internal/storage/migrations.go`
- `web/src/router/index.ts`
- `web/src/layouts/AppLayout.vue`
- `docs/servers.md`

Create:

- `docs/nomad-operations.md`

## Related Interface Docs

- Collaboration index and shared contracts: `00-coordination.md`
- Parent implementation plan: `docs/superpowers/plans/2026-05-24-nomad-control-plane.md`
- Official Nomad API docs copied under `docs/nomad-api-docs/`

## Removal Rules

Remove active references to:

- `container-services`
- `Container Services`
- `containerops`
- `containerrender`
- `containerservice`
- `placement`
- `operation_locks`
- `container_service_placements`
- `panel.claims.ports`
- `docker compose`
- `compose include`
- `root.compose.yaml`

Historical references should not remain in active docs because old design documents are deleted on the Nomad branch.

## Operations Doc Contract

Create `docs/nomad-operations.md` with:

- required Nomad config;
- expected existing Nomad cluster;
- job ID naming;
- meta keys;
- runtime state source;
- how Application deploy, stop, restart, runtime, and logs map to Nomad APIs;
- troubleshooting checklist for connection errors, ACL token errors, no eligible nodes, failed deployments, and failed allocations.

## Steps

- [ ] **Step 1: Remove old packages**

Run:

```bash
git rm -r internal/containerops internal/containerrender internal/containerservice internal/placement
```

Expected: files staged for deletion.

- [ ] **Step 2: Remove old frontend feature**

Run:

```bash
git rm -r web/src/features/container-services
git rm web/src/api/containerServices.ts web/src/api/containerServices.test.ts
```

Expected: files staged for deletion.

- [ ] **Step 3: Scan active code references**

Run:

```bash
rg "container-services|Container Services|containerops|containerrender|containerservice|placement|operation_locks|container_service_placements|panel.claims.ports|root.compose.yaml|compose include|docker compose" internal web docs
```

Expected: matches only in this task file while the cleanup task is still open. Remove all matches in `internal/`, active `web/`, and operational docs.

- [ ] **Step 4: Update scheduler and app wiring**

Remove old container worker arguments and scheduling calls. Keep package, metrics, task, settings, auth, server, and credential wiring.

- [ ] **Step 5: Write Nomad operations docs**

Create `docs/nomad-operations.md` using the operations doc contract above.

- [ ] **Step 6: Run backend verification**

Run:

```bash
go test ./...
```

Expected: PASS.

- [ ] **Step 7: Run frontend verification**

Run:

```bash
cd web
npm test
npm run build
```

Expected: PASS.

- [ ] **Step 8: Final reference scan**

Run:

```bash
rg "container-services|Container Services|containerops|containerrender|containerservice|placement|operation_locks|container_service_placements|panel.claims.ports|root.compose.yaml|compose include|docker compose" internal web docs
```

Expected: no matches in `internal/` or active `web/`. Any doc matches must be in this cleanup task file or intentionally quoted as removal targets.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "refactor: remove compose orchestration layer"
```

## Final Branch Evidence

Before opening PR or merging, collect:

- `go test ./...` output showing PASS.
- `cd web; npm test` output showing PASS.
- `cd web; npm run build` output showing PASS.
- final `rg` scan result and explanation for any historical doc matches.
