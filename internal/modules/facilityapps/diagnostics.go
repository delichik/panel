package facilityapps

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/applications"
	"panel/internal/platform/activitylog"
	panelerr "panel/internal/platform/errors"
	httpx "panel/internal/platform/http"
)

type FacilityDeployment struct {
	ServerID      string                           `json:"serverId"`
	ServerName    string                           `json:"serverName"`
	ObservedState string                           `json:"observedState"`
	ObservedAt    *time.Time                       `json:"observedAt,omitempty"`
	Operation     *applications.LifecycleOperation `json:"operation,omitempty"`
}

// Local projections only: opening or polling configuration never fetches logs.
func (s *Service) deploymentDiagnostics(ctx context.Context, configured []string) ([]FacilityDeployment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.server_id,COALESCE(s.name,''),i.observed_state,i.observed_at,
 COALESCE(j.id,''),COALESCE(j.intent_id,''),COALESCE(j.state,''),COALESCE(j.last_stage,''),COALESCE(j.attempts,0),j.next_run_at,
 COALESCE(j.error_code,''),COALESCE(j.error_class,''),COALESCE(j.error_message,''),COALESCE(j.error_detail,''),j.created_at,j.updated_at,
 COALESCE(j.action,''),COALESCE(j.desired_generation,0),COALESCE(j.desired_spec_hash,''),COALESCE(j.trigger_type,'')
 FROM application_instances i LEFT JOIN servers s ON s.id=i.server_id
 LEFT JOIN jobs j ON j.id=(SELECT x.id FROM jobs x WHERE x.application_id=i.application_id AND x.server_id=i.server_id ORDER BY CASE WHEN x.state IN ('pending','running','failed_retryable') THEN 0 ELSE 1 END,x.created_at DESC,x.id DESC LIMIT 1)
 WHERE i.application_id=? ORDER BY i.server_id`, proxyApplicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FacilityDeployment{}
	seen := map[string]bool{}
	for rows.Next() {
		var d FacilityDeployment
		var op applications.LifecycleOperation
		var observed, next, created, updated sql.NullString
		if err := rows.Scan(&d.ServerID, &d.ServerName, &d.ObservedState, &observed, &op.ID, &op.OperationID, &op.Status, &op.Stage, &op.Attempt, &next, &op.ErrorCode, &op.ErrorClass, &op.Error, &op.ErrorDetail, &created, &updated, &op.Type, &op.Generation, &op.SpecHash, &op.Trigger); err != nil {
			return nil, err
		}
		if observed.Valid && !parseTime(observed.String).IsZero() {
			at := parseTime(observed.String)
			d.ObservedAt = &at
		}
		if op.ID != "" {
			op.ApplicationID = proxyApplicationID
			if next.Valid && !parseTime(next.String).IsZero() {
				at := parseTime(next.String)
				op.NextRunAt = &at
			}
			op.CreatedAt, op.UpdatedAt = parseTime(created.String), parseTime(updated.String)
			op.Error, op.ErrorDetail = safeFacilityEvidence(op.Error, 1024), safeFacilityEvidence(op.ErrorDetail, 8192)
			d.Operation = &op
		}
		seen[d.ServerID] = true
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, serverID := range configured {
		if !seen[serverID] {
			out = append(out, FacilityDeployment{ServerID: serverID, ObservedState: "unknown"})
			seen[serverID] = true
		}
	}
	return out, nil
}

func deploymentPriority(op *applications.LifecycleOperation) int {
	if op == nil {
		return 4
	}
	if op.ErrorClass == "uncertainty" {
		return 7
	}
	switch op.Status {
	case "failed", "cancelled":
		return 6
	case "failed_retryable":
		return 5
	case "pending", "running":
		return 4
	case "succeeded":
		return 1
	}
	return 3
}

func aggregateDeploymentOperation(deployments []FacilityDeployment) *applications.LifecycleOperation {
	var selected *applications.LifecycleOperation
	priority := 0
	for _, d := range deployments {
		if p := deploymentPriority(d.Operation); p > priority || (p == priority && selected == nil && d.Operation != nil) {
			selected, priority = d.Operation, p
		}
	}
	if selected == nil {
		return nil
	}
	copy := *selected
	if copy.ErrorClass == "uncertainty" {
		copy.Status = "needs_attention"
	} else if copy.Status == "pending" || copy.Status == "running" {
		copy.Status = "deploying"
	}
	return &copy
}

type ProxyRequestDiagnostic struct {
	Code     string `json:"code"`
	Domain   string `json:"domain,omitempty"`
	Upstream string `json:"upstream,omitempty"`
	Count    int    `json:"count"`
	LastSeen string `json:"lastSeen,omitempty"`
	Evidence string `json:"evidence,omitempty"`
}

type ProxyDiagnostics struct {
	ServerID  string                   `json:"serverId"`
	CheckedAt time.Time                `json:"checkedAt"`
	Status    string                   `json:"status"`
	Issues    []ProxyRequestDiagnostic `json:"issues"`
	Truncated bool                     `json:"truncated"`
}

type proxyLogClient interface {
	DockerContainerLogs(context.Context, string, string, int) (agentcontract.DockerContainerLogsResponse, error)
}

func (s *Service) DiagnoseReverseProxy(ctx context.Context, serverID string) (ProxyDiagnostics, error) {
	out := ProxyDiagnostics{ServerID: serverID, CheckedAt: time.Now().UTC(), Status: "unavailable", Issues: []ProxyRequestDiagnostic{}}
	var containerID string
	if err := s.db.QueryRowContext(ctx, `SELECT observed_container_id FROM application_instances WHERE application_id=? AND server_id=?`, proxyApplicationID, serverID).Scan(&containerID); err != nil {
		if err == sql.ErrNoRows {
			return out, panelerr.NotFound("facility_application_instance")
		}
		return out, err
	}
	client, ok := s.agent.(proxyLogClient)
	if !ok || containerID == "" {
		return out, nil
	}
	srv, err := s.servers.Get(ctx, serverID)
	if err != nil {
		return out, err
	}
	endpoint := strings.TrimSpace(srv.Traits[agentcontract.TraitURL])
	if endpoint == "" || srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	logs, err := client.DockerContainerLogs(ctx, endpoint, containerID, 200)
	if err != nil {
		return out, nil
	} // Read failure must never alter deployment or retry state.
	if logs.ContainerID != "" && logs.ContainerID != containerID {
		return out, nil
	}
	out.Issues, out.Truncated = parseProxyRequestDiagnostics(logs.Logs)
	out.Status = "sampled"
	return out, nil
}

var proxyServerField = regexp.MustCompile(`, server: ([^,\s]+)`)
var proxyErrorPrefix = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2} \[(?:error|emerg|crit)\] \d+#\d+: (?:\*\d+ )?`)
var proxyUpstreamField = regexp.MustCompile(`upstream: "([^"]+)"`)
var proxyUnresolvedName = regexp.MustCompile(`\*\d+ ([^\s]+) could not be resolved`)

func parseProxyRequestDiagnostics(text string) ([]ProxyRequestDiagnostic, bool) {
	truncated := len(text) > 128<<10
	if truncated {
		text = text[len(text)-(128<<10):]
		if at := strings.IndexByte(text, '\n'); at >= 0 {
			text = text[at+1:]
		}
	}
	out := []ProxyRequestDiagnostic{}
	indices := map[string]int{}
	for _, line := range strings.Split(text, "\n") {
		prefix := proxyErrorPrefix.FindStringIndex(line)
		if prefix == nil {
			continue
		}
		message := strings.SplitN(line[prefix[1]:], ", client:", 2)[0]
		issue := ProxyRequestDiagnostic{Code: "proxy_request_error", Count: 1}
		switch {
		case strings.Contains(message, "could not be resolved"):
			issue.Code = "upstream_name_resolution_failed"
		case strings.Contains(message, "upstream SSL certificate verify error") || strings.Contains(message, "upstream SSL certificate does not match"):
			issue.Code = "upstream_tls_untrusted"
		case strings.Contains(message, "connect() failed"):
			issue.Code = "upstream_connect_failed"
		case strings.Contains(message, "upstream timed out"):
			issue.Code = "upstream_timeout"
		}
		if m := proxyServerField.FindStringSubmatch(line); len(m) > 1 {
			issue.Domain = safeFacilityEvidence(m[1], 255)
		}
		if m := proxyUpstreamField.FindStringSubmatch(line); len(m) > 1 {
			if u, err := url.Parse(m[1]); err == nil {
				issue.Upstream = safeFacilityEvidence(u.Host, 255)
			}
		} else if m := proxyUnresolvedName.FindStringSubmatch(line); len(m) > 1 {
			issue.Upstream = safeFacilityEvidence(m[1], 255)
		}
		if len(line) >= 19 {
			if _, err := time.Parse("2006/01/02 15:04:05", line[:19]); err == nil {
				issue.LastSeen = line[:19]
			}
		}
		// Never return request URLs, query strings, client IPs or credentials.
		evidence := strings.SplitN(line, ", client:", 2)[0]
		if issue.Code == "proxy_request_error" {
			issue.Evidence = ""
		} else {
			issue.Evidence = safeFacilityEvidence(evidence, 512)
		}
		key := issue.Code + "\x00" + issue.Domain + "\x00" + issue.Upstream
		if index, ok := indices[key]; ok {
			out[index].Count++
			out[index].LastSeen = issue.LastSeen
			out[index].Evidence = issue.Evidence
			continue
		}
		if len(out) >= 20 {
			truncated = true
			continue
		}
		indices[key] = len(out)
		out = append(out, issue)
	}
	return out, truncated
}

func safeFacilityEvidence(text string, limit int) string {
	text = activitylog.Redact(strings.ToValidUTF8(text, "�"))
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return text
}

func (h *Handler) ReverseProxyDiagnostics(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	serverID := strings.TrimSpace(query.Get("serverId"))
	if serverID == "" || len(query) != 1 || len(query["serverId"]) != 1 {
		httpx.Error(w, panelerr.Validation("server_required", "Server is required"))
		return
	}
	svc, ok := h.service.(interface {
		DiagnoseReverseProxy(context.Context, string) (ProxyDiagnostics, error)
	})
	if !ok {
		httpx.Error(w, panelerr.Validation("facility_diagnostics_unavailable", "Facility diagnostics are unavailable"))
		return
	}
	result, err := svc.DiagnoseReverseProxy(r.Context(), serverID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, result)
}
