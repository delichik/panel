# Using Panel

English | [简体中文](user-guide.zh-CN.md)

This guide follows the shortest path from a new Panel installation to a running container application. It is task-oriented rather than a complete reference for every field.

If Panel is not running yet, start with [Deploy Panel](deployment.md).

## Understand the Three Layers

- **Panel host:** the machine running the Panel container and the `panel-data` volume.
- **Target server:** a Debian or Ubuntu server registered in Panel and managed over SSH and panel-agent.
- **Application container:** the workload that Panel creates through the target server's Docker Engine.

Panel does not need the target server's Docker Socket mounted into the Panel container. Panel installs its agent on the target server, then connects to that agent over mutual TLS.

## Prepare a Target Server

Before adding a server, confirm that it meets these requirements:

- It runs one of the Debian or Ubuntu versions listed in the project README.
- The Panel host can reach its SSH address and port.
- The selected SSH account is `root` or has passwordless `sudo`.
- Docker is installed and running before you deploy container applications.
- The Panel host can reach TCP port `9786` on the target server after the agent is installed.
- The target server can reach the container registries used by your applications.

Restrict `9786/tcp` to the Panel host where possible. You do not need to expose the target Docker Engine over TCP; the default Docker host is the local Unix Socket `unix:///var/run/docker.sock`.

## 1. First Login

First run `docker exec -it panel /app/panel setup` on the Panel host. It enrolls the current host over SSH, installs the Agent, records the singleton Panel host, and deploys Panel's own domain entrance. Sign in using the URL printed by the command. Setup is a convenience path: you can also skip it and enable the Panel access entry in **Applications → Facility Apps**, choosing a server and domain; the first save registers the chosen server as the Panel host node.

Open the Panel URL and sign in with the initial account:

- Username: `admin`
- Password: `admin`

Panel immediately requires a different password. The password change also rotates the JWT signing secret, so complete this step before adding infrastructure.

After login, review these optional settings:

- **Settings → General:** interface language, token lifetime, metrics retention, collection intervals, and command timeout.
- **Settings → Certificates:** ACME email and certificate defaults, if you plan to issue public certificates.
- **Settings → Security:** administrator account, password, and JWT secret settings.

## 2. Add an SSH Credential

Open **Servers → Credentials**, then select **Add credential**.

Panel supports:

- Username and password.
- Username and private key, with an optional key passphrase.

Give the credential a recognizable name such as `production-root` or `lab-sudo`. One credential can be reused by multiple servers.

For automatic agent installation and privileged maintenance, the account must be `root` or support non-interactive passwordless `sudo`. An account that requires an interactive sudo password cannot perform these operations automatically.

## 3. Add a Server

Open **Servers → Node**, then select **Add server**.

Enter:

- A display name.
- The hostname or IP address reachable from the Panel host.
- The SSH port.
- The credential created in the previous step.
- An optional SSH username override.
- The Docker host. Keep `unix:///var/run/docker.sock` unless Docker uses a different endpoint on that target server.

When the server is saved, Panel starts an initial bootstrap operation that checks SSH access, the operating system, CPU architecture, and privileged command capability. If this first bootstrap fails before the server is initialized, Panel removes the incomplete server entry so you can correct the connection details and add it again.

After a successful bootstrap, Panel automatically schedules panel-agent installation when the server has the required privileges.

## 4. Confirm Agent and Docker Health

Open the new server's details and wait for the initial operations to finish. Before deploying an application, confirm:

- The server is reachable.
- The detected operating system is supported.
- Privilege mode is `root` or passwordless sudo.
- Agent status is compatible.
- Docker is healthy and reports the expected Docker host.

Agent installation creates a service on the target server and listens on TCP port `9786`. If the agent cannot become healthy, check:

- The target firewall and cloud security rules allow the Panel host to reach `9786/tcp`.
- Docker is running on the target server.
- The configured Docker host is correct.
- The SSH account still has the required privileges.
- The operation details and logs in **Task Center**.

If automatic installation has stopped after repeated failures, use the install or reinstall action in the server details after correcting the underlying problem.

## 5. Deploy a First Application

For a simple connectivity test, deploy an Nginx container to one server.

Open **Applications → Applications**, select **Add application**, and configure:

1. Set the application name to `hello-nginx`.
2. Enable the application.
3. Set the image to `nginx:alpine`.
4. Keep network mode set to `bridge`.
5. Add a port mapping from container port `80` to an unused host port such as `8081`.
6. Set deployment mode to selected servers and choose one healthy server.
7. Select **Save and apply**.

Saving an enabled application records the desired state and starts asynchronous coordination. It does not mean every target has finished immediately.

Open the application details to follow each target server's runtime status. Use **Task Center** for deployment stages and logs. A multi-server application can succeed on some servers and fail on others; inspect the failed target instead of assuming the entire operation stopped at the first error.

When the instance is running, open:

```text
http://<target-server>:8081
```

If the target firewall is active, allow the selected host port under **Security → Firewall** or use a reverse proxy instead of publishing the test port publicly.

## 6. Build a Real Application Configuration

The application editor supports visual configuration and YAML. Common sections include:

- Container image and command.
- CPU, memory, privilege, and Linux capability settings.
- Environment variables and reusable Panel variables.
- Port mappings and network mode.
- Host, Docker volume, managed file, and persistent mounts.
- Application files and templates.
- Deployment to all healthy servers or selected servers.
- Reverse-proxy routes.

For the first production application, prefer a single selected server until the image, files, ports, and health behavior are confirmed. Applications with a Panel-managed persistent mount can only target one server.

Use the application detail actions for later changes:

- **Edit:** change the desired configuration.
- **Sync:** reconcile the desired configuration with the observed runtime.
- **Disable:** remove running containers while retaining the application configuration and persistent data.
- **Restart:** request a new runtime application cycle.
- **Delete:** request cleanup of runtime resources and application data. Review destructive confirmations carefully.

Use the runtime log action to inspect container output. If a container starts and exits immediately, the deployment is reported as failed rather than healthy; the runtime and task logs should contain the original Docker or application diagnostic.

## 7. Configure a Domain and HTTPS

Panel currently manages DNS records through Cloudflare and uses ACME DNS-01 challenges for public certificates.

Recommended order:

1. In **Settings → Certificates**, set the ACME email and review certificate defaults.
2. In **DNS → Domains**, add the Cloudflare zone and an API Token.
3. Give the token Zone read permission and DNS record write permission for the target zone.
4. Create or update DNS records so the application domain points to the public address of the gateway server.
5. In **Certificates → Domain certificates**, request the certificate and follow the operation in **Task Center**.
6. In **Applications → Facility Apps**, configure the reverse-proxy gateway servers. These servers need a healthy agent, Docker, and reachable ports `80` and `443`.
7. Edit the application and add a reverse-proxy rule for the domain, path, and application target port.
8. Save and apply, then watch the application and gateway operations until they settle.

Certificate issuance and gateway updates are asynchronous. If issuance fails, inspect the task stages for provider authentication, DNS challenge propagation, authorization, cleanup, or finalization errors.

## 8. Daily Operations

### Overview

Use **Overview** for CPU, memory, disk, network, uptime, load, and other server summaries. Metrics require a healthy compatible agent.

### Package maintenance

Use **Resources → Packages** to refresh APT updates and run selected or full upgrades. Package operations require a healthy agent and privileged access and may take several minutes.

### Firewall

Use **Security → Firewall** to install or manage UFW. When enabling UFW, Panel preserves the configured SSH port first. Review every rule before applying it to a remote server.

### Docker resources

Use **Resources → Containers, Images, Networks,** and **Volumes** to inspect target Docker resources. Resources managed by a Panel application should normally be changed through **Applications**, because direct Docker changes can be reconciled back to the application's desired state.

### Tasks and logs

Use **Task Center** whenever an operation is queued, running, retrying, or failed. An operation can contain multiple tasks, especially when several target servers are involved. Open the specific task to view its steps and logs.

## 9. Backup and Restore

Open **Settings → Backup and restore**.

For export:

1. Keep encryption enabled unless you have a specific reason not to.
2. Request the full export.
3. Panel switches to a maintenance page.
4. Sign in, provide the backup password when requested, and start the export.
5. Download the archive after completion.
6. Exit maintenance mode and wait for Panel to return to normal service.

For restore, upload an existing Panel backup and complete the preflight and confirmation flow. Restore replaces the current instance data; it does not automatically preserve the old state. Create a separate backup before confirming a restore.

Keep the backup password separate from the encrypted archive. Losing either the archive or its password makes the backup unusable.

## 10. Upgrade Safely

Follow the [deployment upgrade instructions](deployment.md#upgrade-panel):

1. Create a fresh full backup.
2. Pull the new image.
3. Recreate the Panel container with the same `panel-data` volume.
4. Check container health and logs.
5. Open **Task Center** and server details. A Panel version change may schedule agent updates so that managed servers use the matching agent version.

## Common Problems

| Symptom | Check |
| --- | --- |
| The default login no longer works | Use the password set during the mandatory first-login change. Restoring or replacing the data volume also changes which account database is active. |
| A new server disappears after saving | The initial SSH/bootstrap operation failed. Check Task Center, then correct the host, port, credential, supported OS, or privilege setup and add it again. |
| Agent is unavailable | Confirm Panel-to-server access on `9786/tcp`, target firewall rules, Docker health, and the server's configured host address. |
| Docker is unhealthy | Start Docker on the target server and verify the configured Docker host, normally `unix:///var/run/docker.sock`. |
| Application deployment stays pending or fails | Open the application runtime target and its task logs. Check agent health, registry access, image name, port conflicts, files, mounts, and container output. |
| The application runs but cannot be reached | Check the target host port, UFW/cloud firewall, DNS record, and reverse-proxy gateway status. |
| Certificate issuance fails | Verify the Cloudflare token permissions, zone name, DNS propagation, ACME email, and task-stage diagnostics. |
| Metrics or Docker resources are stale | Confirm the server's Agent and Docker status and that the Panel host can maintain the agent connection. |

## First-Use Checklist

- [ ] Change the default administrator password.
- [ ] Create an encrypted full backup after initial configuration.
- [ ] Add an SSH credential with root or passwordless sudo access.
- [ ] Add one supported target server.
- [ ] Confirm Agent and Docker health.
- [ ] Deploy a single-server test application.
- [ ] Confirm application logs and network access.
- [ ] Configure HTTPS before exposing Panel or applications publicly.
- [ ] Review Task Center after upgrades or failed operations.
