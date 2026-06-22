package server

import (
	"context"
	"crypto/x509"
	"errors"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	agentsecurity "panel/internal/agent/security"
)

func (s *Service) checkAgent(ctx context.Context, srv Server) error {
	baseURL, ok := agentURL(srv)
	if !ok || s.agent == nil {
		return nil
	}
	health, err := s.agent.Health(ctx, baseURL)
	if err != nil {
		_ = s.handleAgentCertificateTimeError(ctx, srv, err)
		return err
	}
	if strings.TrimSpace(health.Version) != agentcontract.Version {
		_ = s.markAgentStatus(ctx, srv.ID, agentcontract.StatusIncompatible, health.Version, agentVersionMismatchMessage(health.Version))
		return nil
	}
	if health.Docker.Status != "ok" {
		msg := health.Docker.Error
		if msg == "" {
			msg = "docker api is unavailable"
		}
		_ = s.markAgentStatus(ctx, srv.ID, agentcontract.StatusUnavailable, health.Version, msg)
		return nil
	}
	if health.Certificate != nil {
		_ = s.markAgentCertificate(ctx, srv.ID, agentsecurity.CertificateInfo{
			Fingerprint: health.Certificate.Fingerprint,
			CommonName:  health.Certificate.CommonName,
			NotBefore:   health.Certificate.NotBefore,
			NotAfter:    health.Certificate.NotAfter,
		})
		if time.Now().UTC().Add(agentCertificateRenewBefore).After(health.Certificate.NotAfter) {
			_ = s.markAgentStatus(ctx, srv.ID, agentcontract.StatusIncompatible, health.Version, "agent certificate expires soon; redeployment required")
			return nil
		}
	}
	return s.markAgentStatus(ctx, srv.ID, agentcontract.StatusCompatible, health.Version, "")
}

func (s *Service) recoverAgentForSystemDetection(ctx context.Context, srv Server) {
	if agentAutoDeployBlocked(srv) || srv.Traits[agentcontract.TraitStatus] == agentcontract.StatusUndeployable {
		return
	}
	if _, configured := configuredAgentURL(srv); configured && !agentURLMatchesDefault(srv) {
		_ = s.markAgentStatus(ctx, srv.ID, agentcontract.StatusIncompatible, "", nonDefaultAgentURLMessage(srv))
		_, _ = s.ensureAgentDeployTask(context.Background(), srv.ID, "system", true)
		return
	}
	if _, ok := agentURL(srv); !ok {
		_, _ = s.ensureAgentDeployTask(context.Background(), srv.ID, "system", true)
		return
	}
	if expired, msg := agentCertificateRenewalProblem(srv, time.Now()); expired {
		_ = s.markAgentStatus(ctx, srv.ID, agentcontract.StatusIncompatible, "", msg)
		_, _ = s.ensureAgentDeployTask(context.Background(), srv.ID, "system", true)
		return
	}
	if agentStatusNeedsDeploy(srv) {
		_, _ = s.ensureAgentDeployTask(context.Background(), srv.ID, "system", true)
	}
}

func (s *Service) handleAgentCertificateTimeError(ctx context.Context, srv Server, cause error) bool {
	if !isAgentCertificateTimeError(cause) {
		return false
	}
	msg := "agent certificate expired or is not yet valid"
	if cause != nil && strings.TrimSpace(cause.Error()) != "" {
		msg = cause.Error()
	}
	if srv.Traits[agentcontract.TraitStatus] == agentcontract.StatusUndeployable || agentAutoDeployBlocked(srv) {
		_ = s.markAgentStatus(ctx, srv.ID, agentcontract.StatusUndeployable, "", firstNonEmpty(srv.Traits[agentcontract.TraitLastError], msg))
		return true
	}
	if srv.Traits[agentcontract.TraitStatus] == agentcontract.StatusUnavailable {
		_ = s.markAgentStatus(ctx, srv.ID, agentcontract.StatusUnavailable, "", firstNonEmpty(srv.Traits[agentcontract.TraitLastError], msg))
		_, _ = s.ensureAgentDeployTask(context.Background(), srv.ID, "system", true)
		return true
	}
	_ = s.markAgentStatus(ctx, srv.ID, agentcontract.StatusIncompatible, "", msg)
	_, _ = s.ensureAgentDeployTask(context.Background(), srv.ID, "system", true)
	return true
}

func agentCertificateRenewalProblem(srv Server, now time.Time) (bool, string) {
	if value := strings.TrimSpace(srv.Traits[agentcontract.TraitCertificateNotBefore]); value != "" {
		if notBefore, err := time.Parse(time.RFC3339Nano, value); err == nil && now.Before(notBefore) {
			return true, "agent certificate is not yet valid; redeployment required"
		}
	}
	if value := strings.TrimSpace(srv.Traits[agentcontract.TraitCertificateNotAfter]); value != "" {
		if notAfter, err := time.Parse(time.RFC3339Nano, value); err == nil && now.After(notAfter) {
			return true, "agent certificate has expired; redeployment required"
		} else if err == nil && now.Add(agentCertificateRenewBefore).After(notAfter) {
			return true, "agent certificate expires soon; redeployment required"
		}
	}
	return false, ""
}

func agentStatusNeedsDeploy(srv Server) bool {
	return srv.Traits[agentcontract.TraitStatus] == agentcontract.StatusIncompatible
}

func nonDefaultAgentURLMessage(srv Server) string {
	return "agent URL must be " + agentDefaultURL(srv.Host) + "; redeployment required"
}

func agentVersionMismatchMessage(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		version = "unknown"
	}
	return "agent version " + version + " does not match panel " + agentcontract.Version
}

func isAgentCertificateTimeError(err error) bool {
	if err == nil {
		return false
	}
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) && invalid.Reason == x509.Expired {
		return true
	}
	return isAgentCertificateTimeErrorMessage(err.Error())
}

func isAgentCertificateTimeErrorMessage(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "certificate has expired or is not yet valid") ||
		strings.Contains(msg, "certificate has expired") ||
		strings.Contains(msg, "certificate is not yet valid")
}

func agentAutoDeployBlocked(srv Server) bool {
	return traitEnabled(srv.Traits[agentcontract.TraitAutoDeployBlocked])
}

func skipUnavailableAgentScheduledWork(srv Server) bool {
	if agentAutoDeployBlocked(srv) {
		return true
	}
	switch srv.Traits[agentcontract.TraitStatus] {
	case agentcontract.StatusUnavailable, agentcontract.StatusIncompatible, agentcontract.StatusUndeployable:
		return true
	default:
		return false
	}
}
