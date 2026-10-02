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
    cap_add:
      - NET_ADMIN
      - NET_RAW
    devices:
      - /dev/net/tun

volumes:
  panel-data:
    name: panel-data
```

`cap_add` and `devices` are only needed for the optional Tailscale integration; see [Tailscale](#tailscale) below. You can drop them if you do not use it.

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
  --cap-add=NET_ADMIN \
  --cap-add=NET_RAW \
  --device=/dev/net/tun \
  ghcr.io/delichik/panel:latest
```

The three `--cap-add`/`--device` flags are only needed for the optional Tailscale integration; see [Tailscale](#tailscale) below.

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
- Container Tailscale state (node identity, LocalAPI socket and the expected-state file) when the container Tailscale is used.

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

## Firewall takeover during Agent deployment

Seamark manages the server firewall through **UFW only**, and it now does so automatically as a **prerequisite of Agent deployment**: before anything is installed on a node, it installs UFW when missing, allows the SSH port, the Agent port and (when the reverse proxy facility is used) 80/443, and enables the default-deny policy only after the SSH port is allowed. The manual install and enable actions are gone.

Consequences to plan for:

- A node whose distribution has no UFW adapter cannot receive an Agent at all: deployment fails with `agent_firewall_unsupported`. Supported distributions are the same Debian/Ubuntu range as every other package-managing feature.
- **Application ports are not part of that base set.** Each application must declare "open firewall" on the port it publishes; ports published without that flag are blocked. Rules an application wrote earlier stay in the UFW configuration even while UFW is inactive, so enabling the policy applies them.
- Servers added before this behaviour existed are taken over on their next deployment, including certificate renewals.
- A failed takeover fails the deployment but leaves a working Agent untouched.
## Agent delivery over HTTP

Seamark installs `panel-agent` on every managed server. By default it uploads the compressed bundle over the same SSH connection it already uses, which is slow on long or congested routes. You can instead let each server download the bundle over HTTP, which allows a CDN in front of the Panel to cache it.

Set **Settings → Agent download → Agent download base URL** to the address servers should fetch from, for example `https://panel.example.com`. Then:

- The endpoint is `GET /agent/{version}/{platform}/panel-agent.gz`. It is unauthenticated and served outside `/api`, so CDN rules that bypass caching for API traffic do not apply to it. The URL contains the Panel build version, so it can be cached for a long time and a new build simply uses a new URL.
- Servers need to reach that address, plus `gzip` and `sha256sum`. A server with neither `curl` nor a `wget` that has `timeout` gets one repair attempt: on Debian and Ubuntu Seamark installs `curl` from the distribution package manager and retries the download once. If that install fails — most often because the server cannot reach its package mirror — delivery falls back to the SSH upload instead of failing.
- `Agent transfer timeout` (default 300 seconds) bounds the transfer on both sides. It applies to the download and to the SSH upload. The download is split into a few resuming rounds, so a link slower than one round still finishes; raise this value for a slower link rather than expecting a single round to cover it. As a rough guide, a compressed bundle needs about 0.2 Mbit/s to fit the default, and the previous uncompressed 30-second transfer needed about 8 Mbit/s.
- If a download still cannot finish inside the budget, the deployment fails and the task log says so explicitly, naming the timeout to raise. It does not silently fall back, because a download cut in half is a data problem rather than a reachability one.
- **Verify the download TLS certificate** is off by default so a self-signed Panel certificate works. The expected SHA-256 is delivered to the server over the authenticated SSH channel and checked after decompression, so the binary cannot be substituted either way; turning verification on additionally protects the transfer itself.
- If a server cannot reach the address (no fetcher, DNS or connection failure, HTTP error), Seamark falls back to the SSH upload. A truncated transfer, a corrupt archive or a checksum mismatch fails the deployment instead of being retried over the slower path.
- An empty base URL disables HTTP delivery and keeps the previous behaviour.

Note on CDN choice: a free Cloudflare plan usually serves mainland China visitors from overseas edges, so the gain there is limited; a CDN with mainland China edges requires an ICP-filed domain. The origin is always the Panel, fetched on a cache miss.

## Tailscale

Seamark can join the Panel container and the servers it manages to one tailnet, then use tailnet addresses for Panel-to-node and node-to-node connections. Tailscale is **optional**: without the capabilities and device below, everything else keeps working and the feature reports itself as unavailable instead of failing silently.

Container requirements:

- `--cap-add=NET_ADMIN`, `--cap-add=NET_RAW` and `--device=/dev/net/tun`. The Compose examples above use `cap_add` and `devices`; plain `docker run` uses the flags shown in its example.
- The host must provide the `tun` module (`modprobe tun`, or check with `lsmod | grep tun`). Without it `tailscaled` cannot start in kernel TUN mode.
- The container runs `panel-init` as PID 1 as root; `panel-init` drops the Panel process to the non-root `panel` user and manages `tailscaled` itself. State and the LocalAPI socket stay under `/app/data/tailscale`.

Configuration:

- Set the Tailscale auth key in **Settings → Tailscale** ([settings page](https://<panel-host>:8443/settings/tailscale)). The key is write-only: after saving it can no longer be read back, only replaced or cleared. Clearing it removes the stored key and stops the container tailscale.
- ACL tags use the `tag:name` form (lowercase letters, digits and hyphens); they are lowercased, deduplicated and sorted before saving.
- Per-node switches live in the server form and are displayed on the server detail page: join the tailnet, prefer Tailscale for Agent connections, and prefer Tailscale for node interconnect. The two preference switches only take effect when the node has joined and the Panel container itself is logged in; `agent.url` is never rewritten by them.
- **Apply / reconnect** in the same settings section only requests reconciliation. The state shown next to it refreshes once the container tailscale reacts.

Limitations:

- Seamark does not manage UFW rules for the `tailscale0` interface. If UFW is active on a node, allow that interface yourself, otherwise traffic arriving over the tailnet is rejected.
- Nodes that prefer Tailscale for Agent connections need their node certificate to cover the tailnet address. Seamark refreshes it through the existing Agent deployment channel (certificate and configuration only, no binary re-transfer).
- DNS records and application template variables keep using the public address; the tailnet addresses are never published there.

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

### Tailscale shows as unavailable

The settings page says the container tailscale cannot be managed here. Check, in order: the container was started with `--cap-add=NET_ADMIN`, `--cap-add=NET_RAW` and `--device=/dev/net/tun`; the host `tun` module is loaded; the image contains the `tailscale` and `tailscaled` executables. Seamark cannot fix this from inside the container, and the rest of the Panel keeps working; per-node Tailscale intent is still stored.

### Data disappeared after recreating the container

Verify that the container still mounts the original named volume at `/app/data`:

```bash
docker inspect panel --format '{{json .Mounts}}'
docker volume inspect panel-data
```

Continue with [Using Seamark](user-guide.md).
