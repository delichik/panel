# Phase 4 SSL Certificate Design

Source design: `docs/superpowers/specs/2026-05-16-linux-server-panel-design.md`

Phase 4 adds certificate lifecycle management. It depends on Phase 3 domain/provider abstractions and Phase 2 Compose project references, but it must remain provider-neutral so future certificate providers can be added without rewriting Compose or DNS modules.

## Phase Goal

Enable operators to issue, renew, sync, and reference SSL certificates for managed servers and Compose projects.

## Scope

In scope:

- Certificate metadata in the application database.
- Certificate files under `data/certs/`.
- Let's Encrypt certificate issuance.
- Automatic renewal.
- Certificate sync to managed servers over SSH.
- Certificate references usable by Compose projects.
- Task-backed issuance, renewal, and sync logs.

Out of scope:

- Enterprise CA integrations.
- Manual certificate chain editing in the UI.
- Multi-tenant certificate permissions.
- Direct web server configuration beyond syncing files and exposing paths.

## Implementation Split

### Phase 4A: Certificate Issuance

Backend requirements:

- Add certificate metadata schema.
- Add certificate filesystem storage.
- Implement `CertificateProvider`.
- Implement Let's Encrypt issuance.
- Integrate DNS validation through provider-neutral DNS contracts when automation is used.
- Record task stages for account setup, challenge creation, validation wait, issuance, storage, and verification.

Frontend requirements:

- Certificate list and detail pages.
- Certificate request form using domain references.
- Issuance task progress and logs.

Acceptance checks:

- A certificate can be issued for a managed domain.
- Private keys are never returned by APIs or written to logs.
- Failed issuance leaves diagnostic task logs without leaking secrets.

### Phase 4B: Renewal, Sync, and Compose References

Backend requirements:

- Automatic renewal scheduler with configurable threshold.
- Preserve previous valid certificate until new bundle is stored and verified.
- Certificate sync targets per server.
- SSH upload of certificate/key files.
- Remote file permission management.
- Compose project certificate references.
- Optional task-backed project reload after certificate sync.

Frontend requirements:

- Renewal state and expiry indicators.
- Sync target management.
- Sync task progress and logs.
- Compose project certificate reference selector.

Acceptance checks:

- Certificates renew before expiry.
- Sync uploads cert/key files to selected servers.
- Failed renewal or sync does not delete the last known good certificate.
- Compose projects can reference deployed certificate paths.

## Backend Modules

`internal/certs`:

- Owns certificate metadata, storage, issuance, renewal, sync, and references.
- Uses `DNSProvider` abstractions for DNS validation.
- Uses `RemoteExecutor` for server sync.
- Uses `TaskRunner` for every long-running operation.

`internal/dns`:

- Provides domain references and DNS challenge support.

`internal/compose`:

- Consumes certificate references without knowing certificate provider details.

`internal/scheduler`:

- Runs renewal jobs.

## Interfaces

```go
type CertificateProvider interface {
    ID() string
    Issue(ctx context.Context, input CertificateIssueInput, log LogSink) (CertificateBundle, error)
    Renew(ctx context.Context, input CertificateRenewInput, log LogSink) (CertificateBundle, error)
}

type CertificateSyncer interface {
    Sync(ctx context.Context, target Target, cert CertificateBundle, destination CertificateDestination, log LogSink) error
}
```

Rules:

- Certificate private keys never appear in API responses or logs.
- Renewal must preserve the previous valid bundle until the new bundle is verified.
- Server sync uses SSH upload and permission changes through `RemoteExecutor`.
- Compose projects store certificate references, not provider-specific certificate internals.

## API Groups

Certificates:

- `GET /api/v1/certificates`
- `POST /api/v1/certificates/issue`
- `GET /api/v1/certificates/{certificateId}`
- `POST /api/v1/certificates/{certificateId}/renew`
- `DELETE /api/v1/certificates/{certificateId}`

Certificate sync:

- `GET /api/v1/certificates/{certificateId}/sync-targets`
- `POST /api/v1/certificates/{certificateId}/sync-targets`
- `PUT /api/v1/certificates/{certificateId}/sync-targets/{targetId}`
- `POST /api/v1/certificates/{certificateId}/sync`
- `DELETE /api/v1/certificates/{certificateId}/sync-targets/{targetId}`

Compose certificate references:

- `POST /api/v1/compose/projects/{projectId}/certificate-references`
- `DELETE /api/v1/compose/projects/{projectId}/certificate-references/{referenceId}`

## Difficulty and Risk Notes

High-risk areas:

- ACME challenge timing and retries.
- DNS propagation.
- Certificate private-key secrecy.
- Renewal failure recovery.
- Remote file permissions.
- Reloading projects after sync.

Controls:

- Use explicit task stages.
- Store new bundles separately until verification succeeds.
- Preserve previous valid bundles.
- Use staging paths and atomic moves where possible.
- Make project reload after sync optional and explicit.
- Redact ACME account keys, provider tokens, certificate private keys, and passphrases.
