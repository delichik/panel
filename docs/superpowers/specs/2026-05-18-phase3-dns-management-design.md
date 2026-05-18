# Phase 3 DNS Management Design

Source design: `docs/superpowers/specs/2026-05-16-linux-server-panel-design.md`

Phase 3 adds provider-neutral DNS management with Cloudflare as the first provider. It is intentionally separated from SSL issuance so DNS CRUD and provider credential handling can stabilize before certificate automation depends on it.

## Phase Goal

Enable operators to configure DNS providers, list zones, manage DNS records, and create domain references that later certificate and Compose workflows can reuse.

## Scope

In scope:

- Provider credential metadata and secret storage.
- DNS provider registry.
- Cloudflare provider implementation.
- Zone list and zone refresh.
- DNS record list, create, update, and delete.
- DNS record cache in the application database.
- Domain references independent of provider-specific record IDs.

Out of scope:

- Advanced reconciliation loops.
- Multiple provider synchronization.
- Certificate issuance.
- Complex DNS automation policies.

## Implementation Split

### Phase 3A: Provider Framework and Cloudflare CRUD

Backend requirements:

- Add `internal/provider` for provider credentials and secret handling.
- Add `internal/dns` for provider-neutral zones and records.
- Implement `DNSProvider`.
- Implement Cloudflare zones and DNS record CRUD.
- Map provider errors into typed domain errors.
- Cache zones and records after successful provider reads.

Frontend requirements:

- Provider credential page with write-only token input.
- Zone list page.
- Record list and edit page.
- Provider error display without exposing secrets.

Acceptance checks:

- Cloudflare zones can be listed.
- Supported records can be created, edited, and deleted.
- Provider token is never returned by APIs.
- Failed refresh preserves last known cache.

### Phase 3B: Domain References

Backend requirements:

- Create domain reference model independent of provider record IDs.
- Link a domain reference to zone, hostname, and optional DNS record.
- Provide validation helpers for hostname, zone ownership, and record type suitability.

Frontend requirements:

- Domain reference management UI.
- Reusable domain selector component for future certificate screens.

Acceptance checks:

- Domain references survive provider cache refreshes.
- Compose and certificate features can consume domain references without Cloudflare-specific fields.

## Backend Modules

`internal/provider`:

- Owns provider credential metadata and write-only secret storage.
- Provides redacted provider credential DTOs.

`internal/dns`:

- Owns provider-neutral zones, records, cache refresh, and domain references.
- Contains Cloudflare implementation behind `DNSProvider`.

`internal/tasks`:

- Records provider refresh or bulk DNS operations when they are long-running.

`internal/scheduler`:

- May periodically refresh DNS cache when configured.

## Interface

```go
type DNSProvider interface {
    ID() string
    ListZones(ctx context.Context, credential ProviderCredential) ([]DNSZone, error)
    ListRecords(ctx context.Context, credential ProviderCredential, zoneID string) ([]DNSRecord, error)
    CreateRecord(ctx context.Context, credential ProviderCredential, zoneID string, input DNSRecordInput) (DNSRecord, error)
    UpdateRecord(ctx context.Context, credential ProviderCredential, zoneID string, recordID string, input DNSRecordInput) (DNSRecord, error)
    DeleteRecord(ctx context.Context, credential ProviderCredential, zoneID string, recordID string) error
}
```

Rules:

- Provider record IDs are external IDs.
- Internal domain references use panel-generated IDs.
- Provider tokens are write-only.
- Provider-specific validation lives inside the provider implementation.

## API Groups

Provider credentials:

- `GET /api/v1/provider-credentials`
- `POST /api/v1/provider-credentials`
- `PUT /api/v1/provider-credentials/{credentialId}`
- `DELETE /api/v1/provider-credentials/{credentialId}`

DNS:

- `GET /api/v1/dns/providers`
- `GET /api/v1/dns/zones`
- `POST /api/v1/dns/zones/refresh`
- `GET /api/v1/dns/zones/{zoneId}/records`
- `POST /api/v1/dns/zones/{zoneId}/records`
- `PUT /api/v1/dns/zones/{zoneId}/records/{recordId}`
- `DELETE /api/v1/dns/zones/{zoneId}/records/{recordId}`

Domain references:

- `GET /api/v1/domain-references`
- `POST /api/v1/domain-references`
- `PUT /api/v1/domain-references/{referenceId}`
- `DELETE /api/v1/domain-references/{referenceId}`

## Difficulty and Risk Notes

Medium-risk areas:

- Provider API pagination.
- Provider validation differences.
- Rate limits.
- Token handling.
- Record cache consistency after failed writes.

Controls:

- Use mocked provider clients for unit tests.
- Cache only after successful provider response parsing.
- Redact tokens in logs and API responses.
- Keep provider-specific IDs out of generic domain references.
