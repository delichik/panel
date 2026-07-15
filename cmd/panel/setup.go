package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"panel/internal/modules/installation"
	"panel/internal/modules/servers/credential"
	"panel/internal/platform/config"
)

func runSetupCLI(args []string) int {
	flags := flag.NewFlagSet("panel setup", flag.ContinueOnError)
	host := flags.String("host", "", "Panel host SSH address")
	port := flags.Int("port", 22, "Panel host SSH port")
	username := flags.String("user", "root", "Panel host SSH username")
	name := flags.String("name", "Panel host", "managed server name")
	domain := flags.String("domain", "", "Panel entrance domain")
	authType := flags.String("auth", credential.TypePassword, "SSH authentication type: password or private_key")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	reader := bufio.NewReader(os.Stdin)
	*host = prompt(reader, "SSH host", *host)
	*username = prompt(reader, "SSH user", *username)
	portText := prompt(reader, "SSH port", strconv.Itoa(*port))
	if parsed, err := strconv.Atoi(portText); err == nil {
		*port = parsed
	}
	*name = prompt(reader, "Server name", *name)
	*domain = prompt(reader, "Panel domain", *domain)
	*authType = prompt(reader, "Authentication (password/private_key)", *authType)

	input := installation.SetupInput{
		ServerName: *name, Host: *host, Port: *port, Username: *username,
		AuthType: *authType, Domain: *domain,
	}
	if *authType == credential.TypePrivateKey {
		fmt.Fprintln(os.Stdout, "Paste the private key, ending with its -----END ... PRIVATE KEY----- line:")
		var lines []string
		for {
			line, err := reader.ReadString('\n')
			if err != nil && line == "" {
				fmt.Fprintln(os.Stderr, "failed to read private key:", err)
				return 1
			}
			line = strings.TrimRight(line, "\r\n")
			lines = append(lines, line)
			if strings.HasPrefix(line, "-----END ") && strings.HasSuffix(line, " PRIVATE KEY-----") {
				break
			}
		}
		input.PrivateKey = strings.Join(lines, "\n") + "\n"
		input.Passphrase = readSecret("Private key passphrase (empty if none)")
	} else {
		input.Password = readSecret("SSH password")
	}

	cfg, err := config.Load(os.Getenv("PANEL_CONFIG"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "load config failed:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	fmt.Fprintln(os.Stdout, "Setting up the Panel host and entrance gateway...")
	result, err := installation.RunSetupThroughControl(ctx, cfg.DataRoot, input)
	if err != nil {
		fmt.Fprintln(os.Stderr, "setup failed:", err)
		return 1
	}
	fmt.Fprintln(os.Stdout, "Panel entrance is ready:", result.URL)
	return 0
}

func prompt(reader *bufio.Reader, label, defaultValue string) string {
	if defaultValue != "" {
		fmt.Fprintf(os.Stdout, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(os.Stdout, "%s: ", label)
	}
	value, _ := reader.ReadString('\n')
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultValue
	}
	return value
}

func readSecret(label string) string {
	fmt.Fprint(os.Stdout, label+": ")
	value, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return ""
	}
	return string(value)
}
