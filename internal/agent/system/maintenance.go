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
	agentfirewall "panel/internal/agent/firewall"
	panelerr "panel/internal/platform/errors"
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
	updates := linux.ParseAptListUpgradable(out)
	// 安全判定基于临时安全 APT Pinning 模拟 `apt-get -s upgrade --with-new-pkgs`
	// （见 apt_security.go）。判定失败时保持 SecurityKnown=false，绝不影响列表本身。
	applyAptSecurity(ctx, updates)
	return updates, nil
}

func (LocalCollector) UpgradePackages(ctx context.Context, req agentcontract.PackageUpgradeRequest) (string, error) {
	// Package upgrades must survive Panel disconnects; killing apt mid-transaction
	// can leave dpkg/Docker in an inconsistent state.
	execCtx := context.WithoutCancel(ctx)
	if req.SecurityOnly && !req.All {
		return upgradeSecurityPackages(execCtx)
	}
	args := []string{"-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold"}
	if req.All {
		args = append(args, "dist-upgrade", "-y")
	} else {
		if len(req.Names) == 0 {
			return "", panelerr.Validation("packages_required", "At least one package is required")
		}
		args = append(args, "install", "-y", "--only-upgrade")
		for _, name := range req.Names {
			if !packageNamePattern.MatchString(name) {
				return "", panelerr.Validation("package_name_invalid", "Package name contains invalid characters")
			}
			args = append(args, name)
		}
	}
	return runCommand(execCtx, time.Hour, "apt-get", args...)
}

// upgradeSecurityPackages 通过临时安全 APT Pinning 执行仅安全更新（PKG-API-011）：
// 先刷新索引，再在隔离安全 Pin 视图下预检是否有安全更新；为空时直接返回，
// 非空时执行 `apt-get upgrade --with-new-pkgs -y`，不修改系统 `/etc/apt/`。
func upgradeSecurityPackages(ctx context.Context) (string, error) {
	if out, err := runCommand(ctx, 15*time.Minute, "apt-get", "update"); err != nil {
		return out, err
	}
	prefPath, cleanup, err := writeSecurityPreferencesFile(ctx)
	if err != nil {
		return "", err
	}
	defer cleanup()

	inst, err := simulateSecurityUpgrade(ctx, prefPath)
	if err != nil {
		return "", err
	}
	if len(inst) == 0 {
		return "No security updates available\n", nil
	}
	args := securityAptPreferencesArgs(prefPath)
	args = append(args,
		"-o", "Dpkg::Options::=--force-confdef",
		"-o", "Dpkg::Options::=--force-confold",
		"upgrade", "--with-new-pkgs", "-y",
	)
	return runCommand(ctx, time.Hour, "apt-get", args...)
}

func (c LocalCollector) InstallUFW(ctx context.Context, req agentcontract.UFWInstallRequest) (remoteops.UFWStatus, error) {
	err := agentfirewall.WithMutationLock(func() error {
		if _, err := runCommand(ctx, 15*time.Minute, "apt-get", "update"); err != nil {
			return err
		}
		if _, err := runCommand(ctx, 15*time.Minute, "apt-get", "install", "-y", "ufw"); err != nil {
			return err
		}
		for _, rule := range req.Rules {
			if err := runUFWAllow(ctx, rule); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return remoteops.UFWStatus{}, err
	}
	return c.UFWStatus(ctx)
}

func (c LocalCollector) EnableUFW(ctx context.Context, req agentcontract.UFWEnableRequest) (remoteops.UFWStatus, error) {
	err := agentfirewall.WithMutationLock(func() error {
		if _, err := exec.LookPath("ufw"); err != nil {
			if _, installErr := runCommand(ctx, 15*time.Minute, "apt-get", "install", "-y", "ufw"); installErr != nil {
				return installErr
			}
		}
		// Ensuring SSH access is the only operation allowed to touch the SSH
		// port; it must happen before enabling the default-deny firewall.
		if err := runUFWAllow(ctx, remoteops.UFWRule{Port: req.SSHPort, Protocol: "tcp"}); err != nil {
			return err
		}
		agentPort := req.AgentPort
		if agentPort <= 0 || agentPort > 65535 {
			agentPort = 9786
		}
		if err := runUFWAllow(ctx, remoteops.UFWRule{Port: agentPort, Protocol: "tcp"}); err != nil {
			return err
		}
		_, err := runCommand(ctx, time.Minute, "ufw", "--force", "enable")
		return err
	})
	if err != nil {
		return remoteops.UFWStatus{}, err
	}
	return c.UFWStatus(ctx)
}

func (c LocalCollector) AllowUFW(ctx context.Context, req agentcontract.UFWAllowRequest) (remoteops.UFWStatus, error) {
	if err := agentfirewall.ValidateProtectedPort(req.Rule.Port, req.SSHPort, req.AgentPort); err != nil {
		return remoteops.UFWStatus{}, err
	}
	if err := agentfirewall.WithMutationLock(func() error {
		if err := runUFWAllow(ctx, req.Rule); err != nil {
			return err
		}
		protocol := strings.ToLower(strings.TrimSpace(req.Rule.Protocol))
		if protocol == "" {
			protocol = "tcp"
		}
		agentfirewall.ArmPending(agentfirewall.InverseOp{
			Action:   agentfirewall.InverseDeleteAllow,
			Port:     req.Rule.Port,
			Protocol: protocol,
			From:     strings.TrimSpace(req.Rule.From),
		})
		return nil
	}); err != nil {
		return remoteops.UFWStatus{}, err
	}
	return c.UFWStatus(ctx)
}

func (c LocalCollector) DeleteUFW(ctx context.Context, req agentcontract.UFWDeleteRequest) (remoteops.UFWStatus, error) {
	if err := agentfirewall.New().DeleteRule(ctx, req.Number, req.SSHPort, req.AgentPort); err != nil {
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
