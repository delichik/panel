package facilityapps

import (
	"encoding/json"
	"strings"
	"time"

	"panel/internal/modules/applications"
)

const (
	ReverseProxyID       = "reverse_proxy"
	supportedProxyImage  = "nginx:1.28-alpine"
	proxyApplicationID   = "facility-reverse-proxy"
	proxyInstancePrefix  = "facility-reverse-proxy-"
	proxyContainerName   = "panel-facility-reverse-proxy"
	proxyConfigRoot      = "nginx"
	proxyConfigPath      = "nginx/nginx.conf"
	proxyContainerRoot   = "/etc/panel-nginx"
	proxyConfigDir       = "nginx/conf.d"
	proxyStaticMountRoot = "/srv/panel-static"
	proxyTLSMountRoot    = "/etc/panel-certs"

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

	dnsSyncTaskType = "dns_proxy_records_sync"

	DNSSyncPending = "pending"
	DNSSyncSynced  = "synced"
	DNSSyncFailed  = "failed"
	DNSSyncSkipped = "skipped"
)

// DNSSyncState is the per-domain reverse proxy DNS sync status.
type DNSSyncState struct {
	State     string `json:"state"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	Error     string `json:"error,omitempty"`
}

type ReverseProxyConfig struct {
	ID                string                                       `json:"id"`
	Version           int                                          `json:"version"`
	DeploymentServers []string                                     `json:"deploymentServers"`
	PanelHostServerID string                                       `json:"panelHostServerId,omitempty"`
	PanelEntry        PanelEntry                                   `json:"panelEntry"`
	Domains           []FacilityRouteDomain                        `json:"domains"`
	StaticAssets      []StaticAsset                                `json:"staticAssets"`
	RouteSummaries    []RouteSummary                               `json:"routeSummaries"`
	ApplicationRoutes []applications.ApplicationReverseProxyConfig `json:"applicationRoutes"`
	Operation         *applications.LifecycleOperation             `json:"operation,omitempty"`
	ReconcileStopped  bool                                         `json:"reconcileStopped,omitempty"`
	LastError         string                                       `json:"lastError,omitempty"`
	UpdatedAt         time.Time                                    `json:"updatedAt"`
	Routes            int                                          `json:"routes"`
	EnabledServers    []string                                     `json:"enabledServers"`
	DNSSync           map[string]DNSSyncState                      `json:"dnsSync,omitempty"`
}

type ReverseProxySaveInput struct {
	DeploymentServers []string              `json:"deploymentServers"`
	PanelEntry        PanelEntry            `json:"panelEntry"`
	Domains           []FacilityRouteDomain `json:"domains"`
}

type PanelEntry struct {
	Enabled  bool   `json:"enabled"`
	ServerID string `json:"serverId,omitempty"`
	Domain   string `json:"domain,omitempty"`
}

type FacilityRouteDomain struct {
	Domain          string                       `json:"domain"`
	OriginServerIDs []string                     `json:"originServerIds"`
	AnyAccess       applications.AnyAccessConfig `json:"anyAccess"`
	Paths           []FacilityRoutePath          `json:"paths"`
}

type FacilityRoutePath struct {
	Path            string                        `json:"path"`
	RuleType        string                        `json:"ruleType,omitempty"`
	RootPath        string                        `json:"rootPath,omitempty"`
	SourceType      string                        `json:"sourceType"`
	AssetName       string                        `json:"assetName,omitempty"`
	AssetID         string                        `json:"-"`
	RedirectURL     string                        `json:"redirectUrl,omitempty"`
	RedirectCode    int                           `json:"redirectCode,omitempty"`
	ProxyURL        string                        `json:"proxyUrl,omitempty"`
	ProxySourceMode string                        `json:"proxySourceMode,omitempty"`
	Options         applications.HTTPRouteOptions `json:"options,omitempty"`
}

// UnmarshalJSON accepts the pre-name route reference while keeping new
// responses free of the physical asset identifier.
func (p *FacilityRoutePath) UnmarshalJSON(data []byte) error {
	type routePathAlias FacilityRoutePath
	var value routePathAlias
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var legacy struct {
		AssetID string `json:"assetId"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		return err
	}
	*p = FacilityRoutePath(value)
	p.AssetID = legacy.AssetID
	return nil
}

func (p FacilityRoutePath) MarshalJSON() ([]byte, error) {
	type routePathAlias FacilityRoutePath
	value := routePathAlias(p)
	if strings.TrimSpace(value.AssetName) == "" {
		value.AssetName = strings.TrimSpace(value.AssetID)
	}
	value.AssetID = ""
	return json.Marshal(value)
}

type StaticAsset struct {
	// ID is a storage key and is intentionally not part of the public asset
	// contract.  Routes and clients address an asset by its facility-local name.
	ID          string    `json:"-"`
	Name        string    `json:"name"`
	Kind        string    `json:"kind"`
	ContentMode string    `json:"contentMode"`
	Filename    string    `json:"filename"`
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type StaticAssetUploadInput struct {
	AssetID   string
	AssetName string
	Name      string
	Kind      string
	FileName  string
	Size      int64
	Content   []byte
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
	ApplicationID   string   `json:"applicationId,omitempty"`
	ApplicationName string   `json:"applicationName,omitempty"`
}

type ReconcileResult struct {
	Config ReverseProxyConfig `json:"config"`
}

type BeginSaveSessionInput struct {
	BaseUpdatedAt time.Time `json:"baseUpdatedAt"`
}

type CommitSaveSessionInput struct {
	Save ReverseProxySaveInput `json:"save"`
}

type StaticAssetDeleteInput struct {
	AssetName string `json:"assetName,omitempty"`
	// AssetID is accepted only by the legacy in-memory save-session endpoint.
	AssetID string `json:"assetId,omitempty"`
}

type SaveSessionResult struct {
	ID        string        `json:"id"`
	ExpiresAt time.Time     `json:"expiresAt"`
	Assets    []StaticAsset `json:"assets"`
}

type SaveSessionCommitResult struct {
	Config         ReverseProxyConfig `json:"config"`
	ApplyRequested bool               `json:"applyRequested"`
}

const (
	FacilityEditSessionActive     = "active"
	FacilityEditSessionCommitting = "committing"
	FacilityEditSessionCommitted  = "committed"
	FacilityEditSessionConflict   = "conflict"
	FacilityEditSessionDiscarded  = "discarded"
	FacilityEditSessionExpired    = "expired"
)

type FacilityEditAsset struct {
	// AssetKey and SourceAssetID are storage references kept for commit and
	// migration handling; clients use the facility-local name as identity.
	AssetKey      string    `json:"-"`
	SourceAssetID string    `json:"-"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	ContentMode   string    `json:"contentMode"`
	Filename      string    `json:"filename"`
	Size          int64     `json:"size"`
	SHA256        string    `json:"sha256"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

type FacilityEditSession struct {
	ID                  string                       `json:"id"`
	ClientDraftKey      string                       `json:"clientDraftKey,omitempty"`
	State               string                       `json:"state"`
	BaseResourceVersion applications.ResourceVersion `json:"baseResourceVersion"`
	Draft               ReverseProxySaveInput        `json:"draft"`
	Revision            int                          `json:"revision"`
	Assets              []FacilityEditAsset          `json:"assets"`
	PreviewToken        *applications.PreviewToken   `json:"previewToken,omitempty"`
	IdleExpiresAt       time.Time                    `json:"idleExpiresAt"`
	AbsoluteExpiresAt   time.Time                    `json:"absoluteExpiresAt"`
	CreatedAt           time.Time                    `json:"createdAt"`
	UpdatedAt           time.Time                    `json:"updatedAt"`
	CommittedAt         *time.Time                   `json:"committedAt,omitempty"`
	CommitResult        *FacilityEditCommitResult    `json:"commitResult,omitempty"`
}

type BeginFacilityEditSessionInput struct {
	ClientDraftKey string                 `json:"clientDraftKey,omitempty"`
	Draft          *ReverseProxySaveInput `json:"draft,omitempty"`
}

type PatchFacilityEditSessionInput struct {
	Revision            int                   `json:"revision"`
	BaseResourceVersion string                `json:"baseResourceVersion,omitempty"`
	Draft               ReverseProxySaveInput `json:"draft"`
}

type FacilityEditAssetInput struct {
	Revision          int
	ClientOperationID string
	Name              string
	Kind              string
	ContentMode       string
	FileName          string
	Content           []byte
}

type FacilityEditMutationInput struct {
	Revision          int    `json:"revision"`
	ClientOperationID string `json:"clientOperationId"`
}

type FacilityEditValidationResult struct {
	Valid       bool                      `json:"valid"`
	Revision    int                       `json:"revision"`
	Diagnostics []applications.Diagnostic `json:"diagnostics"`
}

type FacilityEditPreviewResult struct {
	Revision    int                       `json:"revision"`
	Diagnostics []applications.Diagnostic `json:"diagnostics"`
	Token       applications.PreviewToken `json:"token"`
	ExpiresAt   time.Time                 `json:"expiresAt"`
}

type CommitFacilityEditSessionInput struct {
	Revision            int    `json:"revision"`
	BaseResourceVersion string `json:"baseResourceVersion"`
	PreviewToken        string `json:"previewToken"`
}

type FacilityEditCommitResult struct {
	Config          ReverseProxyConfig           `json:"config"`
	ResourceVersion applications.ResourceVersion `json:"resourceVersion"`
	ApplyRequested  bool                         `json:"applyRequested"`
	Diagnostics     []applications.Diagnostic    `json:"diagnostics,omitempty"`
}
