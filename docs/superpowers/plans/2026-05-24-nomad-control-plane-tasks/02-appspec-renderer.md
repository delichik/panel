# Task 02: Application Spec Validation And Nomad Job Renderer

**Goal:** Define Panel's Application spec and render it into Nomad JSON jobs.

**Architecture:** `internal/appspec` is a pure domain package. It has no database, HTTP, task, or frontend dependencies. It validates a Panel-owned YAML/JSON spec and renders `nomad.Job` values for the client from Task 01.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, Nomad JSON job syntax from official docs.

---

## Ownership

Primary owner: Backend domain.

Can start after Task 01 publishes stable Nomad DTO names. It can develop against a temporary local DTO copy, but must reconcile before merge.

Blocks:

- Task 04 Application service.
- Task 06 frontend editor validation semantics.

## Official Docs

- `docs/nomad-api-docs/json-jobs.mdx`
- `docs/nomad-api-docs/validate.mdx`
- `docs/nomad-api-docs/jobs.mdx`

## Files

Create:

- `internal/appspec/model.go`
- `internal/appspec/validate.go`
- `internal/appspec/render.go`
- `internal/appspec/hash.go`
- `internal/appspec/render_test.go`
- `internal/appspec/validate_test.go`

Modify:

- `go.mod`
- `go.sum`
- `internal/nomad/types.go` only if renderer needs fields missing from Task 01 DTOs.

## Application Spec Contract

```yaml
name: web
image: nginx:1.27
count: 2
command: []
args: []
env:
  MODE: prod
ports:
  - label: http
    to: 80
    static: 8080
resources:
  cpu: 200
  memoryMb: 128
constraints:
  - attribute: "${node.class}"
    operator: "="
    value: "apps"
services:
  - name: web
    port: http
    tags: ["public"]
checks:
  - name: http
    type: http
    port: http
    path: /
    intervalSeconds: 10
    timeoutSeconds: 2
volumes:
  - source: web-data
    target: /usr/share/nginx/html
    readOnly: false
```

## Validation Rules

- `name`: lowercase letters, digits, and `-`; starts and ends with alphanumeric; length 1-32.
- `image`: required.
- `count`: defaults to `1`; cannot be negative.
- `ports[].label`: same format as `name`.
- `ports[].to`: required when a port exists; 1-65535.
- `ports[].static`: optional; if set, 1-65535.
- `resources.cpu`: defaults to `100`; cannot be negative.
- `resources.memoryMb`: defaults to `128`; cannot be negative.
- `checks[].type`: one of `tcp`, `http`, `script`.
- `checks[].port`: must reference an existing port label for `tcp` and `http`.
- `services[].port`: must reference an existing port label.
- `volumes[].target`: absolute Linux path.
- `volumes[].source`: non-empty when volume exists.

## Render Contract

Panel renders:

- Nomad job ID: `panel-<spec.name>`
- Nomad job name: `<spec.name>`
- Job type: `service`
- Datacenters: single configured datacenter
- Task group: one group named `<spec.name>`
- Task: one Docker task named `<spec.name>`
- Docker image: `task.Config["image"]`
- Network mode: `bridge`
- Static ports -> `ReservedPorts`
- Dynamic ports -> `DynamicPorts`
- Meta keys from `00-coordination.md`

## Steps

- [ ] **Step 1: Write failing renderer test**

Create `internal/appspec/render_test.go` with a test that builds the YAML above, parses it, renders a Nomad job, and asserts job ID, datacenter, task driver, image, count, ports, resources, checks, and meta keys.

- [ ] **Step 2: Write failing validation tests**

Create `internal/appspec/validate_test.go` with tests for invalid name, missing image, invalid port range, check referencing a missing port, service referencing a missing port, and volume target not absolute.

- [ ] **Step 3: Verify failures**

Run: `go test ./internal/appspec`

Expected: FAIL because package does not exist.

- [ ] **Step 4: Implement model**

Create `internal/appspec/model.go` with `Spec`, `Port`, `Resources`, `Constraint`, `Service`, `Check`, `Volume`, and `Issue` types matching the YAML contract.

- [ ] **Step 5: Implement validation and defaults**

Create `internal/appspec/validate.go` with:

```go
func Normalize(spec Spec) Spec
func Validate(spec Spec) []Issue
func DecodeYAML(raw string) (Spec, []Issue)
```

`DecodeYAML` must return validation issues instead of panicking or silently accepting malformed YAML.

- [ ] **Step 6: Implement stable hashing**

Create `internal/appspec/hash.go` with:

```go
func Hash(spec Spec, variables map[string]string) (string, error)
```

Hash normalized spec JSON plus sorted variables JSON with SHA-256. This prevents map iteration order from changing `spec_hash`.

- [ ] **Step 7: Implement renderer**

Create `internal/appspec/render.go` with:

```go
type RenderInput struct {
	AppID      string
	Generation int
	SpecHash   string
	Namespace  string
	Region     string
	Datacenter string
	Spec       Spec
}

func Render(in RenderInput) (nomad.Job, []Issue)
```

Return validation issues when input is invalid. Do not call Nomad here.

- [ ] **Step 8: Run verification**

Run:

```bash
go test ./internal/appspec
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/appspec go.mod go.sum
git commit -m "feat: render application specs as nomad jobs"
```

## Handoff Notes

Give Task 04 owners:

- the exact `DecodeYAML`, `Hash`, and `Render` signatures;
- the exact validation issue shape;
- one rendered job fixture for the sample YAML.
