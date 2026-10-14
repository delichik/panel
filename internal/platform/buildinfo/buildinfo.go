package buildinfo

import "strings"

var (
	Version    = "dev"
	Channel    = "dev"
	Repository = ""
	Commit     = ""
)

func NormalizedVersion() string {
	version := strings.TrimSpace(Version)
	if version == "" {
		return "dev"
	}
	return version
}

func NormalizedChannel() string {
	if strings.EqualFold(strings.TrimSpace(Channel), "release") {
		return "release"
	}
	return "dev"
}
