# Dev Servers

These hosts are disposable development machines. For the Nomad control plane branch, configure them as Nomad servers or clients outside Panel, then point Panel at the existing Nomad HTTP API with `nomad.address`, `nomad.region`, `nomad.namespace`, and `nomad.token`.

See `docs/nomad-operations.md` for Panel runtime expectations and troubleshooting.

## 192.168.242.130

distro: Debian 13

users:

- `du`: `123`
- `root`: `123`

## 192.168.242.131

distro: Debian 12

users:

- `du`: `123`
- `root`: `123`
