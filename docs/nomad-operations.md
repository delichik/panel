# Nomad Operations

Panel uses Nomad as the only runtime control plane. Panel stores Application desired state, renders Nomad JSON jobs, calls the Nomad HTTP API, records tasks, and reads runtime status from Nomad.

## Required Config

Set the Nomad client configuration in `config.json`:

- `nomad.address`: base URL for the Nomad HTTP API, for example `http://127.0.0.1:4646`.
- `nomad.region`: Nomad region used for API requests.
- `nomad.namespace`: Nomad namespace for Panel-managed jobs.
- `nomad.token`: ACL token when the cluster has ACLs enabled.
- `nomad.jobPrefix`: prefix for rendered Application job IDs.

Panel expects an existing Nomad cluster. It does not bootstrap Nomad servers, Nomad clients, drivers, networks, storage, ACL policies, or namespaces.

## Job Identity

Application job IDs are deterministic:

```text
<nomad.jobPrefix><application-name>
```

Application names are normalized before rendering so the job ID is stable across validate, plan, deploy, stop, restart, runtime, and log operations.

Rendered jobs include Panel metadata:

- `panel.application_id`
- `panel.application_name`
- `panel.revision`
- `panel.spec_hash`
- `panel.managed_by`

These keys are for inventory and troubleshooting. Nomad job state remains the source of runtime truth.

## Runtime Source

Panel reads nodes, jobs, allocations, evaluations, deployments, services, checks, and logs from Nomad APIs. Stored Application rows describe intended state only; they are not used as runtime status authority.

## Operation Mapping

- Deploy: validate the Application spec, render a Nomad job, call the job plan API for preview when requested, then register the job through Nomad.
- Stop: issue a Nomad job deregister request for the rendered job ID.
- Restart: create a new task record and ask Nomad to restart through the rendered job/runtime API path used by the Application service.
- Runtime view: query Nomad jobs, allocations, evaluations, deployments, services, checks, and node status.
- Logs: query Nomad allocation logs for the selected allocation and task.

Each operation records a Panel task with status, steps, and sanitized logs for UI display.

## Troubleshooting

Connection errors:

- Confirm `nomad.address` is reachable from the Panel process.
- Check DNS, firewall rules, proxy settings, and Nomad HTTP listener configuration.
- Verify the Nomad cluster is healthy before retrying from Panel.

ACL token errors:

- Confirm `nomad.token` is present when ACLs are enabled.
- Ensure the token can read nodes, jobs, allocations, evaluations, deployments, services, checks, and logs.
- Ensure the token can plan, register, and deregister jobs in the configured namespace.

No eligible nodes:

- Check Nomad client status and node eligibility.
- Confirm required drivers, networks, volumes, and resources exist on clients.
- Inspect the Nomad evaluation details for constraint or resource failures.

Failed deployments:

- Open the deployment details in Panel or Nomad and inspect task group health.
- Check allocation events for image, command, network, service, or check failures.
- Roll forward by editing the Application desired state and deploying a new revision.

Failed allocations:

- Inspect allocation events and task logs from the Panel runtime view.
- Confirm the task driver can pull images and start the configured command.
- Verify service checks, ports, environment variables, templates, and volume mounts.
