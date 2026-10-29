package firewall

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const ownerPrefix = "panel:application:"

var mutationMu sync.Mutex

var (
	addedRulePattern    = regexp.MustCompile(`^ufw\s+allow\s+([0-9]+)(?:/(tcp|udp))?(?:\s+comment\s+['\"]([^'\"]+)['\"])?\s*$`)
	numberedRulePattern = regexp.MustCompile(`^\[\s*([0-9]+)\]\s+(.+?)\s{2,}(ALLOW(?:\s+(?:IN|OUT))?)\s{2,}(.+)$`)
	portPattern         = regexp.MustCompile(`^([0-9]+)(?:/(tcp|udp))?(?:\s+\(v6\))?$`)
)

var ErrNotInstalled = errors.New("UFW is not installed; install it before opening application ports")

type Rule struct {
	Port     int
	Protocol string
}

type commandRunner interface {
	LookPath(string) (string, error)
	Run(context.Context, string, ...string) (string, error)
}

type localCommandRunner struct{}

func (localCommandRunner) LookPath(name string) (string, error) { return exec.LookPath(name) }
func (localCommandRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	out, err := exec.CommandContext(commandCtx, name, args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

type Manager struct{ runner commandRunner }

func New() *Manager { return &Manager{runner: localCommandRunner{}} }

func newWithRunner(runner commandRunner) *Manager { return &Manager{runner: runner} }

func OwnerComment(applicationID string) string { return ownerPrefix + strings.TrimSpace(applicationID) }

func IsManagedComment(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), ownerPrefix)
}

func ValidateProtectedPort(port, sshPort, agentPort int) error {
	if port == normalizeSSHPort(sshPort) {
		return fmt.Errorf("refusing to modify SSH firewall port %d", port)
	}
	if port == normalizeAgentPort(agentPort) {
		return fmt.Errorf("refusing to modify Agent firewall port %d", port)
	}
	return nil
}

// WithMutationLock serializes legacy UFW install/enable mutations with the
// application-rule manager. UFW rule numbers and files are process-global.
func WithMutationLock(fn func() error) error {
	mutationMu.Lock()
	defer mutationMu.Unlock()
	return fn()
}

// EnsureApplicationRules creates only missing application-owned rules. A
// matching manual rule is respected but never adopted or rewritten.
func (m *Manager) EnsureApplicationRules(ctx context.Context, applicationID string, sshPort, agentPort int, desired []Rule) error {
	owner := OwnerComment(applicationID)
	if owner == ownerPrefix {
		return errors.New("application id is required for managed firewall rules")
	}
	wanted, err := normalizeRules(desired, sshPort, agentPort)
	if err != nil {
		return err
	}
	if len(wanted) == 0 {
		return nil
	}
	mutationMu.Lock()
	defer mutationMu.Unlock()
	if err := m.requireInstalled(); err != nil {
		return err
	}
	existing, err := m.addedRules(ctx)
	if err != nil {
		return err
	}
	for _, want := range wanted {
		for _, current := range existing {
			if current.Rule != want || current.Comment == "" || current.Comment == owner {
				continue
			}
			if IsManagedComment(current.Comment) {
				return fmt.Errorf("firewall port %d/%s is managed by another Panel application", want.Port, want.Protocol)
			}
		}
		if containsRule(existing, want) {
			continue
		}
		if _, err := m.runner.Run(ctx, "ufw", "allow", ruleTarget(want), "comment", owner); err != nil {
			return err
		}
		existing = append(existing, addedRule{Rule: want, Comment: owner})
	}
	verified, err := m.addedRules(ctx)
	if err != nil {
		return err
	}
	for _, want := range wanted {
		if !containsRule(verified, want) {
			return fmt.Errorf("firewall rule %d/%s was not present after UFW update", want.Port, want.Protocol)
		}
	}
	return nil
}

// CleanupApplicationRules removes only rules carrying this application's
// owner comment. desired is retained, allowing apply to clean stale mappings
// only after Docker has converged successfully.
func (m *Manager) CleanupApplicationRules(ctx context.Context, applicationID string, sshPort, agentPort int, desired []Rule) error {
	owner := OwnerComment(applicationID)
	if owner == ownerPrefix {
		return errors.New("application id is required for managed firewall rules")
	}
	wanted, err := normalizeRules(desired, sshPort, agentPort)
	if err != nil {
		return err
	}
	mutationMu.Lock()
	defer mutationMu.Unlock()
	if err := m.requireInstalled(); err != nil {
		// No installed UFW means there cannot be a live UFW rule to clean.
		if errors.Is(err, ErrNotInstalled) && len(wanted) == 0 {
			return nil
		}
		return err
	}
	existing, err := m.addedRules(ctx)
	if err != nil {
		return err
	}
	for _, current := range existing {
		if current.Comment != owner || containsExactRule(wanted, current.Rule) {
			continue
		}
		if err := ValidateProtectedPort(current.Port, sshPort, agentPort); err != nil {
			return err
		}
		// Re-read immediately before deletion and require the ownership marker
		// to still be present; never delete from a stale numbered snapshot.
		fresh, err := m.addedRules(ctx)
		if err != nil {
			return err
		}
		if !containsAddedRule(fresh, current) {
			continue
		}
		if _, err := m.runner.Run(ctx, "ufw", "--force", "delete", "allow", ruleTarget(current.Rule), "comment", owner); err != nil {
			return err
		}
		after, err := m.addedRules(ctx)
		if err != nil {
			return err
		}
		if containsAddedRule(after, current) {
			return fmt.Errorf("managed firewall rule %d/%s was still present after UFW deletion", current.Port, current.Protocol)
		}
	}
	return nil
}

func (m *Manager) AllowRule(ctx context.Context, rule Rule, sshPort, agentPort int) error {
	normalized, err := normalizeRules([]Rule{rule}, sshPort, agentPort)
	if err != nil {
		return err
	}
	mutationMu.Lock()
	defer mutationMu.Unlock()
	if err := m.requireInstalled(); err != nil {
		return err
	}
	_, err = m.runner.Run(ctx, "ufw", "allow", ruleTarget(normalized[0]))
	return err
}

func (m *Manager) DeleteRule(ctx context.Context, number, sshPort, agentPort int) error {
	if number <= 0 {
		return errors.New("UFW rule number must be positive")
	}
	mutationMu.Lock()
	defer mutationMu.Unlock()
	if err := m.requireInstalled(); err != nil {
		return err
	}
	before, err := m.numberedRules(ctx)
	if err != nil {
		return err
	}
	target, ok := numberedByNumber(before, number)
	if !ok {
		return errors.New("unable to resolve UFW rule target; refusing deletion")
	}
	if err := ValidateProtectedPort(target.Port, sshPort, agentPort); err != nil {
		return err
	}
	if IsManagedComment(target.Comment) {
		return errors.New("application-managed UFW rules cannot be deleted manually")
	}
	fresh, err := m.numberedRules(ctx)
	if err != nil {
		return err
	}
	verified, ok := numberedByNumber(fresh, number)
	if !ok || verified != target {
		return errors.New("UFW rule changed before deletion; refresh and retry")
	}
	_, err = m.runner.Run(ctx, "ufw", "--force", "delete", strconv.Itoa(number))
	return err
}

func (m *Manager) requireInstalled() error {
	if m == nil || m.runner == nil {
		return errors.New("firewall manager is not configured")
	}
	if _, err := m.runner.LookPath("ufw"); err != nil {
		return ErrNotInstalled
	}
	return nil
}

type addedRule struct {
	Rule
	Comment string
}

func (m *Manager) addedRules(ctx context.Context) ([]addedRule, error) {
	out, err := m.runner.Run(ctx, "ufw", "show", "added")
	if err != nil {
		return nil, err
	}
	var rules []addedRule
	for _, raw := range strings.Split(out, "\n") {
		match := addedRulePattern.FindStringSubmatch(strings.TrimSpace(raw))
		if len(match) == 0 {
			continue
		}
		port, _ := strconv.Atoi(match[1])
		protocol := match[2]
		if protocol == "" {
			protocol = "tcp"
		}
		rules = append(rules, addedRule{Rule: Rule{Port: port, Protocol: protocol}, Comment: strings.TrimSpace(match[3])})
	}
	return rules, nil
}

type numberedRule struct {
	Number   int
	Port     int
	Protocol string
	Action   string
	From     string
	Comment  string
}

func (m *Manager) numberedRules(ctx context.Context) ([]numberedRule, error) {
	out, err := m.runner.Run(ctx, "ufw", "status", "numbered")
	if err != nil {
		return nil, err
	}
	var rules []numberedRule
	for _, raw := range strings.Split(out, "\n") {
		match := numberedRulePattern.FindStringSubmatch(strings.TrimSpace(raw))
		if len(match) == 0 {
			continue
		}
		to := strings.TrimSpace(match[2])
		parsed := portPattern.FindStringSubmatch(to)
		if len(parsed) == 0 {
			continue
		}
		number, _ := strconv.Atoi(match[1])
		port, _ := strconv.Atoi(parsed[1])
		protocol := parsed[2]
		if protocol == "" {
			protocol = "tcp"
		}
		from := strings.TrimSpace(match[4])
		comment := ""
		if index := strings.LastIndex(from, "#"); index >= 0 {
			comment = strings.TrimSpace(from[index+1:])
			from = strings.TrimSpace(from[:index])
		}
		rules = append(rules, numberedRule{Number: number, Port: port, Protocol: protocol, Action: strings.TrimSpace(match[3]), From: from, Comment: comment})
	}
	return rules, nil
}

func normalizeRules(input []Rule, sshPort, agentPort int) ([]Rule, error) {
	seen := map[Rule]struct{}{}
	out := make([]Rule, 0, len(input))
	for _, rule := range input {
		protocol := strings.ToLower(strings.TrimSpace(rule.Protocol))
		if protocol == "" {
			protocol = "tcp"
		}
		if rule.Port <= 0 || rule.Port > 65535 {
			return nil, fmt.Errorf("firewall port must be between 1 and 65535")
		}
		if protocol != "tcp" && protocol != "udp" {
			return nil, fmt.Errorf("firewall protocol must be tcp or udp")
		}
		rule.Protocol = protocol
		if err := ValidateProtectedPort(rule.Port, sshPort, agentPort); err != nil {
			return nil, err
		}
		if _, ok := seen[rule]; ok {
			continue
		}
		seen[rule] = struct{}{}
		out = append(out, rule)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Port == out[j].Port {
			return out[i].Protocol < out[j].Protocol
		}
		return out[i].Port < out[j].Port
	})
	return out, nil
}

func normalizeAgentPort(port int) int {
	if port <= 0 || port > 65535 {
		return 9786
	}
	return port
}

func normalizeSSHPort(port int) int {
	if port <= 0 || port > 65535 {
		return 22
	}
	return port
}

func ruleTarget(rule Rule) string { return strconv.Itoa(rule.Port) + "/" + rule.Protocol }

func containsRule(rules []addedRule, wanted Rule) bool {
	for _, rule := range rules {
		if rule.Rule == wanted {
			return true
		}
	}
	return false
}

func containsExactRule(rules []Rule, wanted Rule) bool {
	for _, rule := range rules {
		if rule == wanted {
			return true
		}
	}
	return false
}

func containsAddedRule(rules []addedRule, wanted addedRule) bool {
	for _, rule := range rules {
		if rule == wanted {
			return true
		}
	}
	return false
}

func numberedByNumber(rules []numberedRule, number int) (numberedRule, bool) {
	for _, rule := range rules {
		if rule.Number == number {
			return rule, true
		}
	}
	return numberedRule{}, false
}
