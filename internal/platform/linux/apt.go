package linux

import (
	"context"
	"regexp"
	"strings"
	"time"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/linux/remoteops"
	"panel/internal/platform/ssh"
)

type aptAdapter struct{}

const (
	packageListTimeout    = 3 * time.Minute
	packageUpgradeTimeout = time.Hour
)

var packageNamePattern = regexp.MustCompile(`^[A-Za-z0-9+_.:-]+$`)

func (aptAdapter) ListUpgradeable(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target) ([]PackageUpdate, error) {
	res, err := exec.ExecSudo(ctx, target, sshx.CommandSpec{
		Command: "apt-get update >/dev/null && apt list --upgradable 2>/dev/null",
		Timeout: packageListTimeout,
	})
	if err != nil {
		return nil, err
	}
	return ParseAptListUpgradable(res.Stdout), nil
}

func (aptAdapter) UpgradeSelected(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target, packages []string, log LogSink) error {
	if len(packages) == 0 {
		return panelerr.Validation("packages_required", "At least one package is required")
	}
	for _, p := range packages {
		if !packageNamePattern.MatchString(p) {
			return panelerr.Validation("package_name_invalid", "Package name contains invalid characters")
		}
	}
	cmd := "DEBIAN_FRONTEND=noninteractive apt-get install -y --only-upgrade " + strings.Join(packages, " ")
	_, err := remoteops.Runner{Exec: exec, Target: target, Log: log}.RunSudoLogged(ctx, cmd, packageUpgradeTimeout)
	return err
}

func (aptAdapter) UpgradeAll(ctx context.Context, exec sshx.RemoteExecutor, target sshx.Target, log LogSink) error {
	_, err := remoteops.Runner{Exec: exec, Target: target, Log: log}.RunSudoLogged(ctx, "DEBIAN_FRONTEND=noninteractive apt-get dist-upgrade -y", packageUpgradeTimeout)
	return err
}

func (aptAdapter) SupportsUFW() bool {
	return true
}

func (aptAdapter) UFWInstallScript() string {
	return remoteops.MustAPTInstallScript("ufw") + "ufw --version\n"
}

func ParseAptListUpgradable(out string) []PackageUpdate {
	var updates []PackageUpdate
	re := regexp.MustCompile(`^([^/\s]+)/([^\s]+)\s+([^\s]+)\s+.*\[upgradable from:\s*([^\]]+)\]`)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Listing...") {
			continue
		}
		m := re.FindStringSubmatch(line)
		if len(m) == 5 {
			updates = append(updates, PackageUpdate{Name: m[1], Source: m[2], CandidateVersion: m[3], InstalledVersion: m[4]})
		}
	}
	return updates
}

func pick(v []int64, i int) int64 {
	if len(v) <= i {
		return 0
	}
	return v[i]
}
