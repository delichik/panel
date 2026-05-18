# Managed Server Prerequisites

Phase 1 supports Debian 12 and Debian 13 only.

Each managed server must provide:

- SSH reachable from the panel host.
- Password authentication or private-key authentication.
- A non-interactive shell suitable for running standard Debian commands.
- Passwordless sudo for privileged package operations.

Required remote commands used by Phase 1:

- `cat /etc/os-release`
- `hostname`
- `uname`
- `date`
- `cut`
- `cat /proc/loadavg`
- `awk`
- `free`
- `df`
- `apt-get`
- `apt`
- `sudo -n`

Passwordless sudo check:

```bash
sudo -n true
```

If that command fails, the server can still be saved and tested for SSH reachability, but package upgrades are blocked until passwordless sudo is configured.

The local test servers listed in `docs/servers.md` can be added through the UI after creating a password credential for user `du`.
