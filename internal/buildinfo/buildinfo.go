package buildinfo

import "strings"

var (
	Version    = "dev"
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
