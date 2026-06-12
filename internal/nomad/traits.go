package nomad

import "strings"

const (
	TraitAdvertiseAddress        = "nomad.advertise_address"
	TraitServerAdvertiseAddress  = "nomad.server_advertise_address"
	TraitReverseProxyEnabled     = "nomad.reverse_proxy.enabled"
	TraitReverseProxyStaticFiles = "nomad.reverse_proxy.static_files"
	TraitReverseProxyStaticSites = "nomad.reverse_proxy.static_sites"
)

func traitBool(traits map[string]string, key string) bool {
	if traits == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(traits[key])) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
