# Deploy Seamark

English | [简体中文](deployment.zh-CN.md)

Seamark is distributed as a container image. Use Docker Compose or Docker to run it.

> Seamark is alpha software. Back up the Seamark data volume before upgrades, and test important changes on non-critical systems first.

## Requirements

- A Linux host with Docker Engine installed.
- Docker Compose plugin for the recommended Compose workflow, or Docker alone for the `docker run` workflow.
- A supported host architecture: `amd64` or `arm64`.
- An available TCP port for the web UI. The examples use HTTPS port `8443`.

The host running Seamark is separate from the target servers that Seamark manages. You do not need to mount the Seamark host's Docker Socket into the Seamark container.

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
      - "127.0.0.1:8443:8443"
    volumes:
      - panel-data:/app/data

volumes:
  panel-data:
    name: panel-data
```

Pull the image and start Seamark:

```bash
docker compose pull
docker compose up -d
```

Check its status:

```bash
docker compose ps
docker compose logs --tail=100 panel
```

After startup, open `https://<host>:8443`. The default certificate is self-signed, so the first browser visit will require a certificate exception. After signing in, use **Settings → Certificates** to configure the Panel domain and select a user-managed TLS certificate.

Default account:

- Username: `admin`
- Password: `admin`

Seamark requires a password change after the first login. Change it immediately before continuing with the [user guide](user-guide.md).

## Deploy with Docker

Create the persistent data volume:

```bash
docker volume create panel-data
```

Start Seamark:

```bash
docker run -d \
  --name panel \
  --restart unless-stopped \
  -p 127.0.0.1:8443:8443 \
  -v panel-data:/app/data \
  ghcr.io/delichik/panel:latest
```

Check its status and logs:

```bash
docker ps --filter name=panel
docker logs --tail=100 panel
```

Open `https://127.0.0.1:8443`, accept the initial self-signed certificate warning, then sign in with `admin/admin` and change the password when prompted.

## Data Persistence

All persistent Seamark state is stored under `/app/data`, including:

- Application, task, and metrics databases.
- SSH credentials and provider credentials stored by Seamark.
- Certificate and key assets.
- Seamark security settings and generated master keys.
- Backup and restore working data.

The examples map `/app/data` to the named volume `panel-data`. Recreating or upgrading the container is safe only when the same volume is reused.

Do not remove `panel-data` unless you intentionally want to erase the Seamark instance.

## Back Up Seamark

The recommended backup path is **Settings → Backup and restore** in the Seamark UI. A full export includes the databases, key material, and required metadata. Keep encrypted backup archives and their passwords in separate safe locations.

The export workflow temporarily switches Seamark into a maintenance page. Sign in there, start the export, download the completed archive, and exit maintenance mode to return to normal operation.

Before an image upgrade, make a fresh full export. For an additional host-level snapshot, stop Seamark and back up the `panel-data` Docker volume with your normal infrastructure backup tooling.

## Upgrade Seamark

Back up Seamark before every upgrade. Database migrations run when the new version starts.

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
  -p 8443:8443 \
  -v panel-data:/app/data \
  ghcr.io/delichik/panel:latest
```

Removing the container does not remove the named volume. Do not run `docker volume rm panel-data` during an upgrade.

## Stop and Start Seamark

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

The examples publish the Panel HTTPS port `8443` only on the host loopback interface. Configure a public domain and user-managed certificate in **Settings → Certificates** before exposing the listener publicly.

For a reverse proxy running on the same host, you can bind Seamark to loopback instead:

```yaml
ports:
  - "127.0.0.1:8443:8443"
```

Terminate HTTPS at the reverse proxy and forward requests to `https://127.0.0.1:8443` (trust the Panel self-signed certificate or configure a user certificate). If the reverse proxy runs in another container, connect both containers through a private Docker network instead of using the loopback binding.

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

### Port 8443 is already in use

Change only the host side of the port mapping. For example, publish Seamark on host port `9080`:

```yaml
ports:
  - "9080:8443"
```

Then open `https://<panel-host>:9080`.

### The page is not reachable from another machine

Check the container status, host firewall, cloud security-group rules, and the published host port. If the mapping uses `127.0.0.1`, it is intentionally reachable only from the Seamark host or a local reverse proxy.

### Data disappeared after recreating the container

Verify that the container still mounts the original named volume at `/app/data`:

```bash
docker inspect panel --format '{{json .Mounts}}'
docker volume inspect panel-data
```

Continue with [Using Seamark](user-guide.md).
