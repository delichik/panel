# Phase 4 SSL Certificate Collaboration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add certificate issuance, renewal, server sync, and Compose certificate references.

**Architecture:** `internal/certs` owns provider-neutral certificate lifecycle. Let's Encrypt is implemented behind `CertificateProvider`, DNS validation uses Phase 3 DNS abstractions, sync uses `RemoteExecutor`, and every issuance/renewal/sync operation uses `TaskRunner`.

**Tech Stack:** Go, SQLite, ACME/Let's Encrypt, SSH, Vue 3, Element Plus, Pinia.

---

## Milestone 4A.1: Certificate Metadata and Storage

**Owner:** Backend Providers

**Files:**

- Create: `internal/certs/model.go`
- Create: `internal/certs/repository.go`
- Create: `internal/certs/storage.go`
- Create: `internal/certs/handler.go`

- [ ] Implement certificate metadata schema.
- [ ] Store certificate bundles under `data/certs`.
- [ ] Return redacted certificate DTOs.
- [ ] Verify private keys never appear in API responses or logs.

## Milestone 4A.2: Let's Encrypt Issuance

**Owner:** Backend Providers

**Files:**

- Create: `internal/certs/provider.go`
- Create: `internal/certs/letsencrypt.go`
- Create: `internal/certs/issue_service.go`

- [ ] Implement `CertificateProvider`.
- [ ] Implement Let's Encrypt issuance.
- [ ] Integrate DNS validation through Phase 3 domain references/provider contracts.
- [ ] Add task stages for challenge creation, validation wait, issuance, storage, and verification.
- [ ] Add issuance failure tests.

## Milestone 4A.3: Certificate UI

**Owner:** Frontend Features

**Files:**

- Create: `web/src/features/certificates/pages/CertificatesPage.vue`
- Create: `web/src/features/certificates/pages/CertificateDetailPage.vue`
- Create: `web/src/features/certificates/components/CertificateRequestForm.vue`
- Create: `web/src/features/certificates/components/CertificateTaskPanel.vue`
- Create: `web/src/features/certificates/api.ts`
- Create: `web/src/features/certificates/types.ts`

- [ ] Implement certificate list/detail pages.
- [ ] Implement request form using domain references.
- [ ] Poll issuance tasks and logs.
- [ ] Show expiry and validation status.

## Milestone 4B.1: Renewal Scheduler

**Owner:** Backend Providers and Backend Platform

**Files:**

- Create: `internal/certs/renewal_service.go`
- Modify: `internal/scheduler/jobs.go`
- Modify: `web/src/features/certificates/*`

- [ ] Implement renewal threshold config.
- [ ] Implement automatic renewal job.
- [ ] Preserve previous valid certificate until the new bundle is verified.
- [ ] Show renewal task history and failure state.

## Milestone 4B.2: Server Sync and Compose References

**Owner:** Backend Providers, Backend Operations, Frontend Features

**Files:**

- Create: `internal/certs/sync_service.go`
- Modify: `internal/compose/model.go`
- Modify: `internal/compose/deployment_service.go`
- Modify: `web/src/features/certificates/*`
- Modify: `web/src/features/compose/*`

- [ ] Implement certificate sync targets.
- [ ] Upload cert/key files through SSH.
- [ ] Set remote file permissions.
- [ ] Add Compose certificate reference selection.
- [ ] Make service reload after sync explicit and task-backed.
- [ ] Verify failed sync preserves the last known good certificate.

## Done Criteria

- [ ] Certificates can be issued for managed domains.
- [ ] Certificates renew before expiry.
- [ ] Certificates can sync to selected servers.
- [ ] Deployed services can reference synced certificate paths.
- [ ] Private keys and provider secrets are never exposed.
