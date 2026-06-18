package server

import (
	"context"
	"errors"
	"strings"

	agentcontract "panel/internal/agent/contract"
	agentsecurity "panel/internal/agent/security"
	"panel/internal/modules/tasks"
	panelerr "panel/internal/platform/errors"
)

func (s *Service) IssueAgentCertificate(ctx context.Context, serverID string) (AgentCertificateBundle, error) {
	if s.agentTLS == nil {
		return AgentCertificateBundle{}, panelerr.Validation("agent_tls_unavailable", "Agent TLS assets are unavailable")
	}
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return AgentCertificateBundle{}, err
	}
	agentURL := agentDefaultURL(srv.Host)
	cert, err := s.agentTLS.IssueServerCertificate("panel-agent-"+srv.ID, []string{srv.Host})
	if err != nil {
		return AgentCertificateBundle{}, err
	}
	return AgentCertificateBundle{
		CA:            string(s.agentTLS.CACertificatePEM()),
		Certificate:   string(cert.CertPEM),
		PrivateKey:    string(cert.KeyPEM),
		ListenAddress: defaultAgentListenAddress,
		AgentURL:      agentURL,
		DockerHost:    normalizeDockerHost(srv.DockerHost),
	}, nil
}

func (s *Service) SystemCertificates(ctx context.Context) ([]SystemCertificate, error) {
	if s.agentTLS == nil {
		return nil, panelerr.Validation("agent_tls_unavailable", "Agent TLS assets are unavailable")
	}
	caInfo, err := s.agentTLS.CAInfo()
	if err != nil {
		return nil, err
	}
	clientInfo, err := s.agentTLS.ClientInfo()
	if err != nil {
		return nil, err
	}
	result := []SystemCertificate{
		systemCertificateFromInfo("agent-ca", "ca_certificate", "Panel Agent CA", caInfo),
		systemCertificateFromInfo("agent-panel-client", "tls_certificate", "Panel Agent client", clientInfo),
	}
	servers, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	for _, srv := range servers {
		if cert, ok := agentServerCertificateFromTraits(srv); ok {
			result = append(result, cert)
		}
	}
	return result, nil
}

func (s *Service) ResetSystemCertificate(ctx context.Context, certificateID string) (tasks.Task, error) {
	switch certificateID {
	case "agent-ca", "agent-panel-client":
		task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
			Type:         agentCertificateResetTaskType,
			ResourceType: agentCertificateResourceType,
			ResourceID:   certificateID,
			TriggeredBy:  "user",
			Summary:      "Resetting system-managed agent certificate",
			Status:       tasks.StatusRunning,
		}, tasks.Trigger{Type: "user", Manual: true})
		if err != nil {
			return tasks.Task{}, err
		}
		if !created {
			return task, nil
		}
		go s.runSystemCertificateReset(context.Background(), task.ID, certificateID)
		return task, nil
	default:
		const prefix = "agent-server:"
		if !strings.HasPrefix(certificateID, prefix) || strings.TrimSpace(strings.TrimPrefix(certificateID, prefix)) == "" {
			return tasks.Task{}, panelerr.NotFound("system certificate")
		}
		return s.ensureAgentDeployTask(ctx, strings.TrimPrefix(certificateID, prefix), "user", true)
	}
}

func (s *Service) runSystemCertificateReset(ctx context.Context, taskID, certificateID string) {
	defer s.tasks.FinishExecution(taskID)
	_ = s.tasks.Advance(ctx, taskID, "regenerating", "regenerating system-managed certificate")
	var err error
	switch certificateID {
	case "agent-ca":
		err = s.agentTLS.ResetAll()
	case "agent-panel-client":
		err = s.agentTLS.ResetClientCertificate()
	}
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	reloader, ok := s.agent.(interface {
		ReloadTLSAssets(*agentsecurity.TLSAssets) error
	})
	if !ok {
		_ = s.tasks.Fail(ctx, taskID, errors.New("agent client does not support TLS reload"))
		return
	}
	if err := reloader.ReloadTLSAssets(s.agentTLS); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	if certificateID == "agent-ca" {
		_ = s.tasks.Advance(ctx, taskID, "redeploying", "queueing agent redeployment for all configured servers")
		servers, listErr := s.List(ctx)
		if listErr != nil {
			_ = s.tasks.Fail(ctx, taskID, listErr)
			return
		}
		for _, srv := range servers {
			if srv.Traits[agentcontract.TraitEnabled] != "true" && strings.TrimSpace(srv.Traits[agentcontract.TraitURL]) == "" {
				continue
			}
			_ = s.clearAgentCertificate(ctx, srv.ID)
			_ = s.markAgentStatus(ctx, srv.ID, agentcontract.StatusIncompatible, "", "agent CA was reset; redeployment required")
			if _, deployErr := s.ensureAgentDeployTask(context.Background(), srv.ID, "system", true); deployErr != nil {
				_ = s.tasks.AppendLog(ctx, taskID, "stderr", "failed to queue agent redeployment for "+srv.Name+": "+deployErr.Error())
			}
		}
	}
	_ = s.tasks.Complete(ctx, taskID, "System-managed agent certificate reset")
}
