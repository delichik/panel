# Deploy Panel

English | [简体中文](deployment.zh-CN.md)

Panel is distributed as a container image. Use Docker Compose or Docker to run it.

> Panel is alpha software. Back up the Panel data volume before upgrades, and test important changes on non-critical systems first.

## Requirements

- A Linux host with Docker Engine installed.
- Docker Compose plugin for the recommended Compose workflow, or Docker alone for the `docker run` workflow.
- A supported host architecture: `amd64` or `arm64`.
- An available TCP port for the web UI. The examples use port `8080`.

The host running Panel is separate from the target servers that Panel manages. You do not need to mount the Panel host's Docker Socket into the Panel container.

## Deploy with Docker Compose

Create a directory for the deployment:

```bash
mkdir -p panel
cd panel
```

Create `compose.yaml`:

```yaml
services:
  panel:
    image: ghcr.io/delichik/panel:latest
    container_name: panel
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - panel-data:/app/data

volumes:
  panel-data:
    name: panel-data
```

Pull the image and start Panel:

```bash
docker compose pull
docker compose up -d
```

Check its status:

```bash
docker compose ps
docker compose logs --tail=100 panel
```

After startup, make sure the Panel domain points to this host, then run on the host over SSH:

```bash
docker exec -it panel /app/panel setup
```

Enter the host IP literal (IPv4 or IPv6), port, user, credential, and Panel domain. Panel connects back to the host over SSH, enrolls it, installs the Agent, records it as the singleton Panel host, and deploys Panel's own Nginx entrance. When setup completes, open the reported `http://<panel-domain>` URL. Setup is a convenience path; you can also sign in to the UI and enable the Panel access entry under **Applications → Facility Apps**, where the first save registers the chosen server as the host node.

Setup is resumable. If Agent or entrance deployment fails, run the command again to continue from the saved stage. Passwords and private-key passphrases are read interactively and should not be passed as command arguments.

Default account:

- Username: `admin`
- Password: `admin`

Panel requires a password change after the first login. Change it immediately before continuing with the [user guide](user-guide.md).

## Deploy with Docker

Create the persistent data volume:

```bash
docker volume create panel-data
```

Start Panel:

```bash
docker run -d \
  --name panel \
  --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v panel-data:/app/data \
  ghcr.io/delichik/panel:latest
```

Check its status and logs:

```bash
docker ps --filter name=panel
docker logs --tail=100 panel
```

Run `docker exec -it panel /app/panel setup`, then sign in to the reported domain with `admin/admin` and change the password when prompted.

## Data Persistence

All persistent Panel state is stored under `/app/data`, including:

- Application, task, and metrics databases.
- SSH credentials and provider credentials stored by Panel.
- Certificate and key assets.
- Panel security settings and generated master keys.
- Backup and restore working data.

The examples map `/app/data` to the named volume `panel-data`. Recreating or upgrading the container is safe only when the same volume is reused.

Do not remove `panel-data` unless you intentionally want to erase the Panel instance.

## Back Up Panel

The recommended backup path is **Settings → Backup and restore** in the Panel UI. A full export includes the databases, key material, and required metadata. Keep encrypted backup archives and their passwords in separate safe locations.

The export workflow temporarily switches Panel into a maintenance page. Sign in there, start the export, download the completed archive, and exit maintenance mode to return to normal operation.

Before an image upgrade, make a fresh full export. For an additional host-level snapshot, stop Panel and back up the `panel-data` Docker volume with your normal infrastructure backup tooling.

## Upgrade Panel

Back up Panel before every upgrade. Database migrations run when the new version starts.

### Docker Compose

If you use `latest`:

```bash
docker compose pull
docker compose up -d
docker compose ps
```

For controlled upgrades, replace `latest` in `compose.yaml` with a specific release tag from the repository releases, then run the same commands.

### Docker

Pull the new image, remove only the old container, and create it again with the same volume:

```bash
docker pull ghcr.io/delichik/panel:latest
docker stop panel
docker rm panel
docker run -d \
  --name panel \
  --restart unless-stopped \
  -p 8080:8080 \
  -v panel-data:/app/data \
  ghcr.io/delichik/panel:latest
```

Removing the container does not remove the named volume. Do not run `docker volume rm panel-data` during an upgrade.

## Stop and Start Panel

With Docker Compose:

```bash
docker compose stop
docker compose start
```

With Docker:

```bash
docker stop panel
docker start panel
```

## Network and HTTPS

The examples publish Panel port `8080` only on the host loopback interface. The public Panel entrance is deployed by the entrance gateway after `panel setup`; the raw Panel port does not need to be exposed publicly.

For a reverse proxy running on the same host, you can bind Panel to loopback instead:

```yaml
ports:
  - "127.0.0.1:8080:8080"
```

Terminate HTTPS at the reverse proxy and forward requests to `http://127.0.0.1:8080`. If the reverse proxy runs in another container, connect both containers through a private Docker network instead of using the loopback binding.

## Troubleshooting

### The container exits or is unhealthy

Inspect the logs:

```bash
docker compose logs --tail=200 panel
```

Or, for Docker:

```bash
docker logs --tail=200 panel
```

Confirm that the `panel-data` volume is writable and that the host architecture is `amd64` or `arm64`.

### Port 8080 is already in use

Change only the host side of the port mapping. For example, publish Panel on host port `9080`:

```yaml
ports:
  - "9080:8080"
```

Then open `http://<panel-host>:9080`.

### The page is not reachable from another machine

Check the container status, host firewall, cloud security-group rules, and the published host port. If the mapping uses `127.0.0.1`, it is intentionally reachable only from the Panel host or a local reverse proxy.

### Data disappeared after recreating the container

Verify that the container still mounts the original named volume at `/app/data`:

```bash
docker inspect panel --format '{{json .Mounts}}'
docker volume inspect panel-data
```

Continue with [Using Panel](user-guide.md).
