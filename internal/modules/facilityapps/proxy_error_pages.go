package facilityapps

import "embed"

//go:embed errors/upstream-unavailable.html
var proxyUpstreamUnavailablePages embed.FS

func proxyUpstreamUnavailablePageContent() []byte {
	content, err := proxyUpstreamUnavailablePages.ReadFile("errors/upstream-unavailable.html")
	if err != nil {
		return nil
	}
	return content
}
