package system

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/platform/linux"
	"panel/internal/platform/linux/remoteops"
)

var packageNamePattern = regexp.MustCompile(`^[A-Za-z0-9+_.:-]+$`)
var fail2banNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

const fail2banPanelConfigPath = "/etc/fail2ban/jail.d/panel.local"

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

func (c LocalCollector) Fail2BanStatus(ctx context.Context) (agentcontract.Fail2BanStatusResponse, error) {
	if _, err := exec.LookPath("fail2ban-client"); err != nil {
		return agentcontract.Fail2BanStatusResponse{Installed: false, Active: false}, nil
	}
	out, err := runCommand(ctx, time.Minute, "fail2ban-client", "status")
	if err != nil {
		return agentcontract.Fail2BanStatusResponse{}, err
	}
	return agentcontract.Fail2BanStatusResponse{
		Installed: true,
		Active:    true,
		Jails:     parseFail2BanJails(out),
		Raw:       out,
	}, nil
}

func (c LocalCollector) ApplyFail2Ban(ctx context.Context, req agentcontract.Fail2BanApplyRequest) (agentcontract.Fail2BanStatusResponse, error) {
	if err := validateFail2BanConfig(req.Config); err != nil {
		return agentcontract.Fail2BanStatusResponse{}, err
	}
	if _, err := exec.LookPath("fail2ban-client"); err != nil {
		if _, updateErr := runCommand(ctx, 15*time.Minute, "apt-get", "update"); updateErr != nil {
			return agentcontract.Fail2BanStatusResponse{}, updateErr
		}
		if _, installErr := runCommand(ctx, 15*time.Minute, "apt-get", "install", "-y", "fail2ban"); installErr != nil {
			return agentcontract.Fail2BanStatusResponse{}, installErr
		}
	}
	rendered, err := renderFail2BanConfig(req.Config)
	if err != nil {
		return agentcontract.Fail2BanStatusResponse{}, err
	}
	if err := os.MkdirAll("/etc/fail2ban/jail.d", 0755); err != nil {
		return agentcontract.Fail2BanStatusResponse{}, err
	}
	previous, previousErr := os.ReadFile(fail2banPanelConfigPath)
	if err := os.WriteFile(fail2banPanelConfigPath, []byte(rendered), 0644); err != nil {
		return agentcontract.Fail2BanStatusResponse{}, err
	}
	if _, err := runCommand(ctx, time.Minute, "fail2ban-client", "-t"); err != nil {
		restoreFail2BanConfig(previous, previousErr)
		return agentcontract.Fail2BanStatusResponse{}, err
	}
	if _, err := exec.LookPath("systemctl"); err == nil {
		if _, err := runCommand(ctx, 2*time.Minute, "systemctl", "enable", "--now", "fail2ban"); err != nil {
			return agentcontract.Fail2BanStatusResponse{}, err
		}
		if _, err := runCommand(ctx, 2*time.Minute, "systemctl", "restart", "fail2ban"); err != nil {
			return agentcontract.Fail2BanStatusResponse{}, err
		}
	} else if _, err := runCommand(ctx, time.Minute, "fail2ban-client", "reload"); err != nil {
		return agentcontract.Fail2BanStatusResponse{}, err
	}
	return c.Fail2BanStatus(ctx)
}

func restoreFail2BanConfig(previous []byte, previousErr error) {
	if previousErr == nil {
		_ = os.WriteFile(fail2banPanelConfigPath, previous, 0644)
		return
	}
	if os.IsNotExist(previousErr) {
		_ = os.Remove(fail2banPanelConfigPath)
	}
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

func validateFail2BanConfig(config agentcontract.Fail2BanConfig) error {
	if len(config.Jails) == 0 {
		return errors.New("at least one fail2ban jail is required")
	}
	seen := map[string]struct{}{}
	for _, jail := range config.Jails {
		name := strings.TrimSpace(jail.Name)
		if name == "" || !fail2banNamePattern.MatchString(name) {
			return errors.New("fail2ban jail name is invalid")
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("fail2ban jail %q is duplicated", name)
		}
		seen[name] = struct{}{}
		for key, value := range fail2banFieldValues(jail) {
			if hasLineBreak(value) {
				return fmt.Errorf("fail2ban %s for jail %q must be a single line", key, name)
			}
		}
		keys := make([]string, 0, len(jail.Options))
		for key := range jail.Options {
			key = strings.TrimSpace(key)
			if key == "" || !fail2banNamePattern.MatchString(key) {
				return fmt.Errorf("fail2ban option key %q is invalid", key)
			}
			keys = append(keys, key)
		}
		for _, key := range keys {
			if hasLineBreak(jail.Options[key]) {
				return fmt.Errorf("fail2ban option %q for jail %q must be a single line", key, name)
			}
		}
	}
	return nil
}

func renderFail2BanConfig(config agentcontract.Fail2BanConfig) (string, error) {
	if err := validateFail2BanConfig(config); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("# Managed by Panel. Edit from Panel instead of modifying this file directly.\n")
	for _, jail := range config.Jails {
		name := strings.TrimSpace(jail.Name)
		b.WriteString("\n[")
		b.WriteString(name)
		b.WriteString("]\n")
		writeFail2BanLine(&b, "enabled", boolFail2Ban(jail.Enabled))
		for _, item := range []struct {
			key   string
			value string
		}{
			{"filter", jail.Filter},
			{"logpath", jail.LogPath},
			{"backend", jail.Backend},
			{"port", jail.Port},
			{"protocol", jail.Protocol},
			{"action", jail.Action},
			{"findtime", jail.FindTime},
			{"bantime", jail.BanTime},
		} {
			writeFail2BanLine(&b, item.key, item.value)
		}
		if jail.MaxRetry > 0 {
			writeFail2BanLine(&b, "maxretry", strconv.Itoa(jail.MaxRetry))
		}
		if len(jail.IgnoreIP) > 0 {
			writeFail2BanLine(&b, "ignoreip", strings.Join(jail.IgnoreIP, " "))
		}
		keys := make([]string, 0, len(jail.Options))
		for key := range jail.Options {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			writeFail2BanLine(&b, key, jail.Options[key])
		}
	}
	return b.String(), nil
}

func fail2banFieldValues(jail agentcontract.Fail2BanJail) map[string]string {
	return map[string]string{
		"filter":   jail.Filter,
		"logpath":  jail.LogPath,
		"backend":  jail.Backend,
		"port":     jail.Port,
		"protocol": jail.Protocol,
		"action":   jail.Action,
		"findtime": jail.FindTime,
		"bantime":  jail.BanTime,
		"ignoreip": strings.Join(jail.IgnoreIP, " "),
	}
}

func writeFail2BanLine(b *strings.Builder, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	b.WriteString(key)
	b.WriteString(" = ")
	b.WriteString(value)
	b.WriteString("\n")
}

func boolFail2Ban(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func hasLineBreak(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}

func parseFail2BanJails(raw string) []string {
	for _, line := range strings.Split(raw, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok || !strings.Contains(strings.ToLower(key), "jail list") {
			continue
		}
		parts := strings.Split(value, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if item := strings.TrimSpace(part); item != "" {
				out = append(out, item)
			}
		}
		return out
	}
	return []string{}
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
