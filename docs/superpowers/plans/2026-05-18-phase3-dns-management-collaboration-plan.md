# Phase 3 DNS Management Collaboration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add provider-neutral DNS management with Cloudflare as the first provider and domain references for later certificate workflows.

**Architecture:** Provider credentials are stored through a redacted provider credential layer. `internal/dns` owns provider-neutral DNS behavior and wraps Cloudflare behind `DNSProvider`.

**Tech Stack:** Go, SQLite, Cloudflare API, Vue 3, Element Plus, Pinia.

---

## Milestone 3A.1: Provider Credentials

**Owner:** Backend Providers and Frontend Features

**Files:**

- Create: `internal/provider/model.go`
- Create: `internal/provider/repository.go`
- Create: `internal/provider/secret_store.go`
- Create: `internal/provider/handler.go`
- Create: `web/src/features/dns/components/ProviderCredentialForm.vue`

- [ ] Implement provider credential metadata.
- [ ] Implement write-only provider secret storage.
- [ ] Implement redacted provider credential APIs.
- [ ] Verify tokens are never returned by APIs.

## Milestone 3A.2: Cloudflare DNS Provider

**Owner:** Backend Providers

**Files:**

- Create: `internal/dns/model.go`
- Create: `internal/dns/provider.go`
- Create: `internal/dns/cloudflare.go`
- Create: `internal/dns/service.go`
- Create: `internal/dns/handler.go`

- [ ] Implement `DNSProvider`.
- [ ] Implement Cloudflare zone listing.
- [ ] Implement record list, create, update, and delete.
- [ ] Implement provider error mapping and cache update rules.
- [ ] Add mocked provider tests.

## Milestone 3A.3: DNS UI

**Owner:** Frontend Features

**Files:**

- Create: `web/src/features/dns/pages/DNSProvidersPage.vue`
- Create: `web/src/features/dns/pages/DNSZonesPage.vue`
- Create: `web/src/features/dns/pages/DNSRecordsPage.vue`
- Create: `web/src/features/dns/components/DNSRecordForm.vue`
- Create: `web/src/features/dns/api.ts`
- Create: `web/src/features/dns/types.ts`

- [ ] Implement provider credential management.
- [ ] Implement zone refresh.
- [ ] Implement record CRUD.
- [ ] Handle provider errors and rate-limit states.

## Milestone 3B.1: Domain References

**Owner:** Backend Providers and Frontend Features

**Files:**

- Create: `internal/dns/domain_reference_service.go`
- Create: `web/src/features/dns/components/DomainReferenceSelector.vue`
- Create: `web/src/features/dns/pages/DomainReferencesPage.vue`

- [ ] Implement provider-independent domain references.
- [ ] Implement hostname and zone validation.
- [ ] Implement reusable domain selector component.
- [ ] Verify references survive provider cache refresh.

## Done Criteria

- [ ] Cloudflare zones and records can be managed.
- [ ] Provider tokens are write-only and redacted.
- [ ] Domain references do not depend on Cloudflare-specific record IDs.
- [ ] DNS models can be reused by certificate issuance.
