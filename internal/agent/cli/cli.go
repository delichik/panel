// Package cli implements the read-only panel-agent CLI (--cli apps ...).
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	agentcontract "panel/internal/agent/contract"
	agentdocker "panel/internal/agent/docker"
)

const (
	managedLabelKey   = "panel.application.managed"
	managedLabelValue = "true"

	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// containerSource is the subset of the Docker runtime the CLI needs.
type containerSource interface {
	Containers(ctx context.Context) ([]agentcontract.DockerContainer, error)
	ApplicationHome(applicationID string) (string, error)
	InstanceDir(applicationID, instanceID string) (string, error)
	PersistentDir(applicationID string) (string, error)
}

type runtimeFactory func(dockerHost string) (containerSource, error)

// Run executes the CLI and returns a process exit code.
func Run(args []string) int {
	return run(args, os.Stdout, os.Stderr, func(host string) (containerSource, error) {
		return agentdocker.NewLocalRuntime(host)
	})
}

func run(args []string, stdout, stderr io.Writer, newRuntime runtimeFactory) int {
	if len(args) == 0 {
		printUsage(stderr)
		return exitUsage
	}
	switch args[0] {
	case "help", "-h", "--help":
		printUsage(stdout)
		return exitOK
	case "apps":
		return runApps(args[1:], stdout, stderr, newRuntime)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		printUsage(stderr)
		return exitUsage
	}
}

func runApps(args []string, stdout, stderr io.Writer, newRuntime runtimeFactory) int {
	if len(args) == 0 {
		printAppsUsage(stderr)
		return exitUsage
	}
	switch args[0] {
	case "help", "-h", "--help":
		printAppsUsage(stdout)
		return exitOK
	case "list":
		return runList(args[1:], stdout, stderr, newRuntime)
	case "inspect":
		return runInspect(args[1:], stdout, stderr, newRuntime)
	case "where":
		return runWhere(args[1:], stdout, stderr, newRuntime)
	default:
		fmt.Fprintf(stderr, "unknown apps command %q\n\n", args[0])
		printAppsUsage(stderr)
		return exitUsage
	}
}

type commandFlags struct {
	fs         *flag.FlagSet
	dockerHost string
	jsonOut    bool
}

func parseCommandFlags(name string, args []string, withJSON bool) (*commandFlags, error) {
	cf := &commandFlags{fs: flag.NewFlagSet(name, flag.ContinueOnError)}
	cf.fs.SetOutput(io.Discard)
	cf.fs.StringVar(&cf.dockerHost, "docker-host", "", "Docker Engine host override")
	if withJSON {
		cf.fs.BoolVar(&cf.jsonOut, "json", false, "emit JSON")
	}
	if err := cf.fs.Parse(normalizeArgs(args)); err != nil {
		return nil, err
	}
	return cf, nil
}

// normalizeArgs moves positional arguments after flags so flag.Parse can
// handle flags placed anywhere on the command line.
func normalizeArgs(args []string) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			positional = append(positional, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(arg, "-") && arg != "-":
			flags = append(flags, arg)
			if needsValue(arg) {
				if i+1 < len(args) {
					i++
					flags = append(flags, args[i])
				}
			}
		default:
			positional = append(positional, arg)
		}
	}
	return append(flags, positional...)
}

func needsValue(arg string) bool {
	name := strings.TrimLeft(arg, "-")
	if eq := strings.IndexByte(name, '='); eq >= 0 {
		return false
	}
	return name == "docker-host"
}

func resolveDockerHost(flagHost string) string {
	if strings.TrimSpace(flagHost) != "" {
		return flagHost
	}
	if env := os.Getenv("PANEL_AGENT_DOCKER_HOST"); strings.TrimSpace(env) != "" {
		return env
	}
	return agentcontract.DefaultDockerHost
}

func loadManagedContainers(flagHost string, newRuntime runtimeFactory, stderr io.Writer) ([]agentcontract.DockerContainer, int) {
	src, err := newRuntime(resolveDockerHost(flagHost))
	if err != nil {
		fmt.Fprintf(stderr, "connect to docker: %v\n", err)
		return nil, exitError
	}
	items, err := src.Containers(context.Background())
	if err != nil {
		fmt.Fprintf(stderr, "list containers: %v\n", err)
		return nil, exitError
	}
	return sortManagedContainers(items), exitOK
}

func sortManagedContainers(items []agentcontract.DockerContainer) []agentcontract.DockerContainer {
	out := make([]agentcontract.DockerContainer, 0, len(items))
	for _, c := range items {
		if c.Labels[managedLabelKey] == managedLabelValue {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return containerName(out[i]) < containerName(out[j])
	})
	return out
}

func runList(args []string, stdout, stderr io.Writer, newRuntime runtimeFactory) int {
	cf, err := parseCommandFlags("apps list", args, true)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printAppsUsage(stdout)
			return exitOK
		}
		fmt.Fprintf(stderr, "apps list: %v\n", err)
		return exitUsage
	}
	if cf.fs.NArg() != 0 {
		fmt.Fprintf(stderr, "apps list: unexpected arguments: %s\n", strings.Join(cf.fs.Args(), " "))
		return exitUsage
	}
	items, code := loadManagedContainers(cf.dockerHost, newRuntime, stderr)
	if code != exitOK {
		return code
	}
	if cf.jsonOut {
		return writeJSON(stdout, stderr, items)
	}
	return writeListTable(stdout, stderr, items)
}

func runInspect(args []string, stdout, stderr io.Writer, newRuntime runtimeFactory) int {
	cf, err := parseCommandFlags("apps inspect", args, true)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printAppsUsage(stdout)
			return exitOK
		}
		fmt.Fprintf(stderr, "apps inspect: %v\n", err)
		return exitUsage
	}
	if cf.fs.NArg() != 1 {
		fmt.Fprintf(stderr, "apps inspect: expected exactly one selector\n")
		return exitUsage
	}
	items, code := loadManagedContainers(cf.dockerHost, newRuntime, stderr)
	if code != exitOK {
		return code
	}
	container, err := resolveSelector(items, cf.fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "apps inspect: %v\n", err)
		return exitError
	}
	src, err := newRuntime(resolveDockerHost(cf.dockerHost))
	if err != nil {
		fmt.Fprintf(stderr, "connect to docker: %v\n", err)
		return exitError
	}
	paths, err := resolvePaths(src, container)
	if err != nil {
		fmt.Fprintf(stderr, "apps inspect: %v\n", err)
		return exitError
	}
	out := inspectOutput{
		DockerContainer: container,
		Panel:           panelInfoFrom(container.Labels),
		Paths:           paths,
	}
	if cf.jsonOut {
		return writeJSON(stdout, stderr, out)
	}
	return writeInspectTable(stdout, stderr, out)
}

func runWhere(args []string, stdout, stderr io.Writer, newRuntime runtimeFactory) int {
	home, code := resolveHomeCommand("apps where", args, stdout, stderr, newRuntime)
	if code != exitOK {
		return code
	}
	fmt.Fprintln(stdout, home)
	return exitOK
}

func resolveHomeCommand(name string, args []string, stdout, stderr io.Writer, newRuntime runtimeFactory) (string, int) {
	cf, err := parseCommandFlags(name, args, false)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printAppsUsage(stdout)
			return "", exitOK
		}
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return "", exitUsage
	}
	if cf.fs.NArg() != 1 {
		fmt.Fprintf(stderr, "%s: expected exactly one selector\n", name)
		return "", exitUsage
	}
	items, code := loadManagedContainers(cf.dockerHost, newRuntime, stderr)
	if code != exitOK {
		return "", code
	}
	src, err := newRuntime(resolveDockerHost(cf.dockerHost))
	if err != nil {
		fmt.Fprintf(stderr, "%s: connect to docker: %v\n", name, err)
		return "", exitError
	}
	home, err := resolveHomeAndCheck(src, items, cf.fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		return "", exitError
	}
	return home, exitOK
}

func resolveSelector(items []agentcontract.DockerContainer, selector string) (agentcontract.DockerContainer, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return agentcontract.DockerContainer{}, errors.New("selector is required")
	}
	for _, c := range items {
		for _, name := range c.Names {
			if name == selector || strings.TrimPrefix(name, "/") == selector {
				return c, nil
			}
		}
	}
	for _, c := range items {
		if c.Labels["panel.application.instance.id"] == selector {
			return c, nil
		}
	}
	var appMatches []agentcontract.DockerContainer
	for _, c := range items {
		if c.Labels["panel.application.id"] == selector {
			appMatches = append(appMatches, c)
		}
	}
	switch len(appMatches) {
	case 0:
		return agentcontract.DockerContainer{}, fmt.Errorf("no panel-managed container matches %q", selector)
	case 1:
		return appMatches[0], nil
	default:
		return agentcontract.DockerContainer{}, fmt.Errorf("application %q matches %d instances; use a container name or instance id", selector, len(appMatches))
	}
}

func resolveHomeAndCheck(src containerSource, items []agentcontract.DockerContainer, selector string) (string, error) {
	container, err := resolveSelector(items, selector)
	if err != nil {
		return "", err
	}
	appID := strings.TrimSpace(container.Labels["panel.application.id"])
	if appID == "" {
		return "", errors.New("container has no panel application id")
	}
	home, err := src.ApplicationHome(appID)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(home)
	if err != nil {
		return "", fmt.Errorf("application home %q: %w", home, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("application home %q is not a directory", home)
	}
	return home, nil
}

func resolvePaths(src containerSource, container agentcontract.DockerContainer) (pathsInfo, error) {
	appID := strings.TrimSpace(container.Labels["panel.application.id"])
	if appID == "" {
		return pathsInfo{}, errors.New("container has no panel application id")
	}
	home, err := src.ApplicationHome(appID)
	if err != nil {
		return pathsInfo{}, err
	}
	instanceDir, err := src.InstanceDir(appID, strings.TrimSpace(container.Labels["panel.application.instance.id"]))
	if err != nil {
		return pathsInfo{}, err
	}
	persistentDir, err := src.PersistentDir(appID)
	if err != nil {
		return pathsInfo{}, err
	}
	return pathsInfo{Home: home, InstanceDir: instanceDir, PersistentDir: persistentDir}, nil
}

type inspectOutput struct {
	agentcontract.DockerContainer
	Panel panelInfo `json:"panel"`
	Paths pathsInfo `json:"paths"`
}

type panelInfo struct {
	ApplicationID     string `json:"applicationId,omitempty"`
	InstanceID        string `json:"instanceId,omitempty"`
	Generation        string `json:"generation,omitempty"`
	SpecHash          string `json:"specHash,omitempty"`
	ApplyMode         string `json:"applyMode,omitempty"`
	ManagedFilesHash  string `json:"managedFilesHash,omitempty"`
	ManagedFilesDrift string `json:"managedFilesDrift,omitempty"`
	ManagedFilesError string `json:"managedFilesError,omitempty"`
}

type pathsInfo struct {
	Home          string `json:"home"`
	InstanceDir   string `json:"instanceDir,omitempty"`
	PersistentDir string `json:"persistentDir,omitempty"`
}

func panelInfoFrom(labels map[string]string) panelInfo {
	return panelInfo{
		ApplicationID:     labels["panel.application.id"],
		InstanceID:        labels["panel.application.instance.id"],
		Generation:        labels["panel.application.generation"],
		SpecHash:          labels["panel.application.spec.hash"],
		ApplyMode:         labels["panel.application.apply.mode"],
		ManagedFilesHash:  labels["panel.application.managed_files.hash"],
		ManagedFilesDrift: labels["panel.application.managed_files.drift"],
		ManagedFilesError: labels["panel.application.managed_files.error"],
	}
}

func writeJSON(stdout, stderr io.Writer, v any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(stderr, "write output: %v\n", err)
		return exitError
	}
	return exitOK
}

func writeListTable(stdout, stderr io.Writer, items []agentcontract.DockerContainer) int {
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\tAPPLICATION\tINSTANCE\tIMAGE\tSTATE\tSTATUS\tPORTS\tCREATED")
	for _, c := range items {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			shortID(c.ID),
			containerName(c),
			c.Labels["panel.application.id"],
			c.Labels["panel.application.instance.id"],
			c.Image,
			c.State,
			c.Status,
			formatPorts(c.Ports),
			formatCreated(c.Created),
		)
	}
	if err := tw.Flush(); err != nil {
		fmt.Fprintf(stderr, "write output: %v\n", err)
		return exitError
	}
	return exitOK
}

func writeInspectTable(stdout, stderr io.Writer, out inspectOutput) int {
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	rows := [][2]string{
		{"ID", out.ID},
		{"NAME", strings.Join(out.Names, ", ")},
		{"IMAGE", out.Image},
		{"IMAGE ID", out.ImageID},
		{"STATE", out.State},
		{"STATUS", out.Status},
		{"CREATED", formatCreated(out.Created)},
		{"COMMAND", out.Command},
		{"PORTS", formatPorts(out.Ports)},
		{"MOUNTS", formatMounts(out.Mounts)},
		{"APPLICATION ID", out.Panel.ApplicationID},
		{"INSTANCE ID", out.Panel.InstanceID},
		{"GENERATION", out.Panel.Generation},
		{"SPEC HASH", out.Panel.SpecHash},
		{"APPLY MODE", out.Panel.ApplyMode},
		{"MANAGED FILES HASH", out.Panel.ManagedFilesHash},
		{"MANAGED FILES DRIFT", out.Panel.ManagedFilesDrift},
		{"MANAGED FILES ERROR", out.Panel.ManagedFilesError},
		{"HOME", out.Paths.Home},
		{"INSTANCE DIR", out.Paths.InstanceDir},
		{"PERSISTENT DIR", out.Paths.PersistentDir},
	}
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\n", row[0], row[1])
	}
	if err := tw.Flush(); err != nil {
		fmt.Fprintf(stderr, "write output: %v\n", err)
		return exitError
	}
	return exitOK
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func containerName(c agentcontract.DockerContainer) string {
	if len(c.Names) == 0 {
		return c.ID
	}
	return strings.TrimPrefix(c.Names[0], "/")
}

func formatPorts(ports []agentcontract.DockerPort) string {
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		if p.PublicPort != 0 {
			host := p.IP
			if host == "" {
				host = "0.0.0.0"
			}
			parts = append(parts, fmt.Sprintf("%s:%d->%d/%s", host, p.PublicPort, p.PrivatePort, p.Type))
		} else {
			parts = append(parts, fmt.Sprintf("%d/%s", p.PrivatePort, p.Type))
		}
	}
	return strings.Join(parts, ", ")
}

func formatMounts(mounts []agentcontract.DockerMount) string {
	parts := make([]string, 0, len(mounts))
	for _, m := range mounts {
		parts = append(parts, m.Source+":"+m.Destination)
	}
	return strings.Join(parts, ", ")
}

func formatCreated(ts int64) string {
	if ts <= 0 {
		return ""
	}
	return time.Unix(ts, 0).Local().Format("2006-01-02 15:04:05")
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `panel-agent --cli <command> [args] [flags]

Read-only CLI for containers managed by Panel.

Commands:
  apps list [--json] [--docker-host <host>]   List Panel-managed containers
  apps inspect <selector> [--json] [...]      Show one container with Panel metadata and paths
  apps where <selector> [...]                 Print the application home directory
  help                                        Show this help

Selectors match a container name, an instance id, or an application id
that matches exactly one instance.

Run "panel-agent --cli apps help" for apps command details.
`)
}

func printAppsUsage(w io.Writer) {
	fmt.Fprint(w, `panel-agent --cli apps <command> [args] [flags]

Commands:
  list [--json] [--docker-host <host>]
        List Panel-managed containers.
  inspect <selector> [--json] [--docker-host <host>]
        Show one container's details, Panel metadata and paths.
  where <selector> [--docker-host <host>]
        Print the application home directory path.

Flags:
  --docker-host <host>   Docker Engine host override
                         (default: $PANEL_AGENT_DOCKER_HOST, then unix:///var/run/docker.sock)
  --json                 Emit JSON instead of a table (list, inspect)

Selectors:
  A container name, an instance id, or an application id that matches
  exactly one instance.
`)
}
