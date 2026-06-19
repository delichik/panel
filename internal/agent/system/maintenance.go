package system

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/platform/linux"
	"panel/internal/platform/linux/remoteops"
)

var packageNamePattern = regexp.MustCompile(`^[A-Za-z0-9+_.:-]+$`)

func (LocalCollector) PackageUpdates(ctx context.Context) ([]linux.PackageUpdate, error) {
	if _, err := runCommand(ctx, 15*time.Minute, "apt-get", "update"); err != nil {
		return nil, err
	}
	out, err := runCommand(ctx, 3*time.Minute, "apt", "list", "--upgradable")
	if err != nil {
		return nil, err
	}
	return linux.ParseAptListUpgradable(out), nil
}

func (LocalCollector) UpgradePackages(ctx context.Context, req agentcontract.PackageUpgradeRequest) (string, error) {
	args := []string{"-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold"}
	if req.All {
		args = append(args, "dist-upgrade", "-y")
	} else {
		if len(req.Names) == 0 {
			return "", errors.New("at least one package is required")
		}
		args = append(args, "install", "-y", "--only-upgrade")
		for _, name := range req.Names {
			if !packageNamePattern.MatchString(name) {
				return "", errors.New("package name contains invalid characters")
			}
			args = append(args, name)
		}
	}
	return runCommand(ctx, time.Hour, "apt-get", args...)
}

func (c LocalCollector) InstallUFW(ctx context.Context, req agentcontract.UFWInstallRequest) (remoteops.UFWStatus, error) {
	if _, err := runCommand(ctx, 15*time.Minute, "apt-get", "update"); err != nil {
		return remoteops.UFWStatus{}, err
	}
	if _, err := runCommand(ctx, 15*time.Minute, "apt-get", "install", "-y", "ufw"); err != nil {
		return remoteops.UFWStatus{}, err
	}
	for _, rule := range req.Rules {
		if err := runUFWAllow(ctx, rule); err != nil {
			return remoteops.UFWStatus{}, err
		}
	}
	return c.UFWStatus(ctx)
}

func (c LocalCollector) EnableUFW(ctx context.Context, req agentcontract.UFWEnableRequest) (remoteops.UFWStatus, error) {
	if _, err := exec.LookPath("ufw"); err != nil {
		if _, installErr := runCommand(ctx, 15*time.Minute, "apt-get", "install", "-y", "ufw"); installErr != nil {
			return remoteops.UFWStatus{}, installErr
		}
	}
	if err := runUFWAllow(ctx, remoteops.UFWRule{Port: req.SSHPort, Protocol: "tcp"}); err != nil {
		return remoteops.UFWStatus{}, err
	}
	if _, err := runCommand(ctx, time.Minute, "ufw", "--force", "enable"); err != nil {
		return remoteops.UFWStatus{}, err
	}
	return c.UFWStatus(ctx)
}

func (c LocalCollector) AllowUFW(ctx context.Context, req agentcontract.UFWAllowRequest) (remoteops.UFWStatus, error) {
	if err := runUFWAllow(ctx, req.Rule); err != nil {
		return remoteops.UFWStatus{}, err
	}
	return c.UFWStatus(ctx)
}

func (c LocalCollector) DeleteUFW(ctx context.Context, req agentcontract.UFWDeleteRequest) (remoteops.UFWStatus, error) {
	if req.Number <= 0 {
		return remoteops.UFWStatus{}, errors.New("UFW rule number must be positive")
	}
	if _, err := runCommand(ctx, time.Minute, "ufw", "--force", "delete", strconv.Itoa(req.Number)); err != nil {
		return remoteops.UFWStatus{}, err
	}
	return c.UFWStatus(ctx)
}

func (LocalCollector) RestartSystem(ctx context.Context) error {
	// Call logind's D-Bus API directly. The Agent service runs as root, so no
	// interactive polkit exchange is required.
	_, err := runCommand(ctx, 15*time.Second, "busctl", "call",
		"org.freedesktop.login1",
		"/org/freedesktop/login1",
		"org.freedesktop.login1.Manager",
		"Reboot",
		"b",
		"true",
	)
	return err
}

func runUFWAllow(ctx context.Context, rule remoteops.UFWRule) error {
	if rule.Port <= 0 || rule.Port > 65535 {
		return errors.New("UFW port must be between 1 and 65535")
	}
	protocol := strings.ToLower(strings.TrimSpace(rule.Protocol))
	if protocol == "" {
		protocol = "tcp"
	}
	if protocol != "tcp" && protocol != "udp" && protocol != "any" {
		return errors.New("UFW protocol must be tcp, udp, or any")
	}
	from := strings.TrimSpace(rule.From)
	var args []string
	if from == "" || strings.EqualFold(from, "anywhere") {
		value := strconv.Itoa(rule.Port)
		if protocol != "any" {
			value += "/" + protocol
		}
		args = []string{"allow", value}
	} else {
		args = []string{"allow", "from", from, "to", "any", "port", strconv.Itoa(rule.Port)}
		if protocol != "any" {
			args = append(args, "proto", protocol)
		}
	}
	_, err := runCommand(ctx, time.Minute, "ufw", args...)
	return err
}
