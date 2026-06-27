package facilityapps

import (
	"time"

	"panel/internal/modules/applications"
)

const (
	ReverseProxyID        = "reverse_proxy"
	defaultProxyImage     = "nginx:1.27-alpine"
	defaultPanelUpstream  = "http://127.0.0.1:8080"
	proxyApplicationID    = "facility-reverse-proxy"
	proxyInstancePrefix   = "facility-reverse-proxy-"
	proxyContainerName    = "panel-facility-reverse-proxy"
	proxyConfigPath       = "nginx.conf"
	proxyContainerConf    = "/etc/nginx/nginx.conf"
	proxyConfigDir        = "conf.d"
	proxyContainerConfDir = "/etc/nginx/conf.d"
	proxyStaticMountRoot  = "/srv/panel-static"
	proxyTLSMountRoot     = "/etc/nginx/panel-certs"

	StaticSourceHostPath       = "host_path"
	StaticSourceUploadedFile   = "uploaded_file"
	StaticSourceUploadedBundle = "uploaded_bundle"

	StaticRuleStatic    = "static"
	StaticRuleRedirect  = "redirect"
	StaticRuleProxyPass = "proxy_pass"

	ProxySourcePreserve = "preserve_source"
	ProxySourceHide     = "hide_source"

	HTTPSDomainCertificate = "domain_certificate"
	HTTPSSelfSigned        = "self_signed_certificate"
	HTTPSDisabled          = "disabled"
)

type ReverseProxyConfig struct {
	ID                string                           `json:"id"`
	DeploymentServers []string                         `json:"deploymentServers"`
	Image             string                           `json:"image"`
	PanelEntry        PanelEntry                       `json:"panelEntry"`
	StaticSites       []StaticSite                     `json:"staticSites"`
	StaticAssets      []StaticAsset                    `json:"staticAssets"`
	RouteSummaries    []RouteSummary                   `json:"routeSummaries"`
	Operation         *applications.LifecycleOperation `json:"operation,omitempty"`
	LastError         string                           `json:"lastError,omitempty"`
	UpdatedAt         time.Time                        `json:"updatedAt"`
	Routes            int                              `json:"routes"`
	EnabledServers    []string                         `json:"enabledServers"`
}

type ReverseProxySaveInput struct {
	DeploymentServers []string     `json:"deploymentServers"`
	Image             string       `json:"image"`
	PanelEntry        PanelEntry   `json:"panelEntry"`
	StaticSites       []StaticSite `json:"staticSites"`
}

type PanelEntry struct {
	Enabled  bool   `json:"enabled"`
	ServerID string `json:"serverId,omitempty"`
	Domain   string `json:"domain,omitempty"`
}

type StaticSite struct {
	Domain            string   `json:"domain"`
	Path              string   `json:"path"`
	RuleType          string   `json:"ruleType,omitempty"`
	RootPath          string   `json:"rootPath,omitempty"`
	SourceType        string   `json:"sourceType"`
	AssetID           string   `json:"assetId,omitempty"`
	RedirectURL       string   `json:"redirectUrl,omitempty"`
	RedirectCode      int      `json:"redirectCode,omitempty"`
	ProxyURL          string   `json:"proxyUrl,omitempty"`
	ProxySourceMode   string   `json:"proxySourceMode,omitempty"`
	DeploymentServers []string `json:"deploymentServers,omitempty"`
}

type StaticAsset struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Filename  string    `json:"filename"`
	Size      int64     `json:"size"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type StaticAssetUploadInput struct {
	Name     string
	Kind     string
	FileName string
	Size     int64
	Content  []byte
}

type RouteSummary struct {
	Domain          string   `json:"domain"`
	Path            string   `json:"path"`
	Source          string   `json:"source"`
	ServerIDs       []string `json:"serverIds"`
	HTTPSStatus     string   `json:"httpsStatus"`
	CertificateID   string   `json:"certificateId,omitempty"`
	CertificateName string   `json:"certificateName,omitempty"`
	MatchedDomains  []string `json:"matchedDomains,omitempty"`
}

type ReconcileResult struct {
	Config ReverseProxyConfig `json:"config"`
}
