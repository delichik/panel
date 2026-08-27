package installation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/facilityapps"
	"panel/internal/modules/servers"
	"panel/internal/modules/servers/credential"
	"panel/internal/modules/tasks"
)

type SetupInput struct {
	ServerName string `json:"serverName"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	AuthType   string `json:"authType"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
	Domain     string `json:"domain"`
}

type SetupResult struct {
	HostServerID string `json:"hostServerId"`
	URL          string `json:"url"`
	Stage        string `json:"stage"`
}

type CredentialManager interface {
	Create(ctx context.Context, req credential.CreateRequest) (credential.Credential, error)
	Delete(ctx context.Context, credentialID string) error
}

type ServerManager interface {
	Create(ctx context.Context, req server.SaveRequest) (server.Server, error)
	Get(ctx context.Context, serverID string) (server.Server, error)
	DeployAgent(ctx context.Context, serverID string) (tasks.Task, error)
}

type TaskReader interface {
	Get(ctx context.Context, taskID string) (tasks.Task, error)
}

type FacilityManager interface {
	GetReverseProxy(ctx context.Context) (facilityapps.ReverseProxyConfig, error)
	SaveReverseProxy(ctx context.Context, input facilityapps.ReverseProxySaveInput) (facilityapps.ReverseProxyConfig, error)
}

type SetupService struct {
	installation *Service
	credentials  CredentialManager
	servers      ServerManager
	tasks        TaskReader
	facility     FacilityManager
}

func NewSetupService(installation *Service, credentials CredentialManager, servers ServerManager, taskReader TaskReader, facility FacilityManager) *SetupService {
	return &SetupService{installation: installation, credentials: credentials, servers: servers, tasks: taskReader, facility: facility}
}

func (s *SetupService) Run(ctx context.Context, input SetupInput) (SetupResult, error) {
	input = normalizeSetupInput(input)
	if err := validateSetupInput(input); err != nil {
		return SetupResult{}, err
	}
	state, err := s.installation.Get(ctx)
	if err != nil {
		return SetupResult{}, err
	}
	var srv server.Server
	createdCredentialID := ""
	if state.HostServerID != "" {
		srv, err = s.servers.Get(ctx, state.HostServerID)
	} else if state.PendingServerID != "" {
		srv, err = s.servers.Get(ctx, state.PendingServerID)
	}
	if err != nil {
		return SetupResult{}, err
	}
	if srv.ID == "" {
		cred, createErr := s.credentials.Create(ctx, credential.CreateRequest{
			Name: "Panel host setup", Type: input.AuthType, Username: input.Username,
			Password: input.Password, PrivateKey: input.PrivateKey, Passphrase: input.Passphrase,
		})
		if createErr != nil {
			return SetupResult{}, createErr
		}
		createdCredentialID = cred.ID
		ipv4, ipv6 := server.SplitAddress(input.Host)
		srv, err = s.servers.Create(ctx, server.SaveRequest{
			Name: input.ServerName, IPv4: ipv4, IPv6: ipv6, Port: input.Port, SSHUsername: input.Username,
			CredentialID: cred.ID, DockerHost: agentcontract.DefaultDockerHost,
		})
		if err != nil {
			_ = s.credentials.Delete(ctx, cred.ID)
			return SetupResult{}, err
		}
		if _, err := s.installation.SetPendingServer(ctx, srv.ID, "server_bootstrap"); err != nil {
			return SetupResult{}, err
		}
		if srv.InitialTaskID != "" {
			if _, err := s.waitTask(ctx, srv.InitialTaskID); err != nil {
				_ = s.installation.RecordFailure(ctx, "server_bootstrap", err)
				if createdCredentialID != "" {
					_ = s.credentials.Delete(ctx, createdCredentialID)
				}
				return SetupResult{}, err
			}
		}
	}

	srv, err = s.servers.Get(ctx, srv.ID)
	if err != nil {
		return SetupResult{}, err
	}
	if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
		_, _ = s.installation.SetPendingServer(ctx, srv.ID, "agent_deploy")
		task, deployErr := s.servers.DeployAgent(ctx, srv.ID)
		if deployErr != nil {
			_ = s.installation.RecordFailure(ctx, "agent_deploy", deployErr)
			return SetupResult{}, deployErr
		}
		if task.ID != "" {
			if _, err := s.waitTask(ctx, task.ID); err != nil {
				_ = s.installation.RecordFailure(ctx, "agent_deploy", err)
				return SetupResult{}, err
			}
		}
		srv, err = s.servers.Get(ctx, srv.ID)
		if err != nil {
			return SetupResult{}, err
		}
		if srv.Traits[agentcontract.TraitStatus] != agentcontract.StatusCompatible {
			err = errors.New("Panel Agent did not become compatible")
			_ = s.installation.RecordFailure(ctx, "agent_deploy", err)
			return SetupResult{}, err
		}
	}

	if _, err := s.installation.SetHostServer(ctx, srv.ID); err != nil {
		return SetupResult{}, err
	}
	cfg, err := s.facility.GetReverseProxy(ctx)
	if err != nil {
		return SetupResult{}, err
	}
	servers := append([]string(nil), cfg.DeploymentServers...)
	if !contains(servers, srv.ID) {
		servers = append(servers, srv.ID)
	}
	previousOperationID := ""
	if cfg.Operation != nil {
		previousOperationID = cfg.Operation.ID
	}
	_, err = s.facility.SaveReverseProxy(ctx, facilityapps.ReverseProxySaveInput{
		DeploymentServers: servers,
		PanelEntry:        facilityapps.PanelEntry{Enabled: true, ServerID: srv.ID, Domain: input.Domain},
		Domains:           cfg.Domains,
	})
	if err != nil {
		_ = s.installation.RecordFailure(ctx, "proxy_deploy", err)
		return SetupResult{}, err
	}
	if err := s.waitFacility(ctx, previousOperationID); err != nil {
		_ = s.installation.RecordFailure(ctx, "proxy_deploy", err)
		return SetupResult{}, err
	}
	return SetupResult{HostServerID: srv.ID, URL: "http://" + input.Domain, Stage: "complete"}, nil
}

func (s *SetupService) waitTask(ctx context.Context, taskID string) (tasks.Task, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		task, err := s.tasks.Get(ctx, taskID)
		if err != nil {
			return tasks.Task{}, err
		}
		switch task.Status {
		case tasks.StatusCompleted:
			return task, nil
		case tasks.StatusFailed, tasks.StatusBlocked, tasks.StatusCancelled:
			return task, fmt.Errorf("task %s %s: %s", task.ID, task.Status, firstNonEmpty(task.Error, task.Summary))
		}
		select {
		case <-ctx.Done():
			return tasks.Task{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *SetupService) waitFacility(ctx context.Context, previousOperationID string) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		cfg, err := s.facility.GetReverseProxy(ctx)
		if err != nil {
			return err
		}
		if cfg.Operation != nil && cfg.Operation.ID != previousOperationID {
			switch cfg.Operation.Status {
			case "succeeded":
				return nil
			case "failed":
				return fmt.Errorf("Panel entrance gateway deployment %s: %s", cfg.Operation.Status, cfg.Operation.Error)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func normalizeSetupInput(input SetupInput) SetupInput {
	input.ServerName = strings.TrimSpace(input.ServerName)
	input.Host = strings.TrimSpace(input.Host)
	input.Username = strings.TrimSpace(input.Username)
	input.AuthType = strings.TrimSpace(input.AuthType)
	input.Domain = strings.ToLower(strings.TrimSpace(input.Domain))
	if input.Port == 0 {
		input.Port = 22
	}
	if input.ServerName == "" {
		input.ServerName = "Panel host"
	}
	return input
}

func validateSetupInput(input SetupInput) error {
	if input.Host == "" || input.Username == "" || input.Domain == "" {
		return errors.New("SSH host, username, and Panel domain are required")
	}
	if input.Port < 1 || input.Port > 65535 {
		return errors.New("SSH port must be between 1 and 65535")
	}
	if input.AuthType != credential.TypePassword && input.AuthType != credential.TypePrivateKey {
		return errors.New("SSH authentication type must be password or private_key")
	}
	return nil
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
