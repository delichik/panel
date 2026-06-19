package server

import (
	"panel/internal/modules/servers/domain"
	"panel/internal/platform/ssh"
)

type Server = domain.Server
type ArchitectureInfo = domain.ArchitectureInfo
type SudoState = domain.SudoState
type PrivilegeState = domain.PrivilegeState
type SaveRequest = domain.SaveRequest
type ProbeResult = domain.ProbeResult
type UFWState = domain.UFWState
type UFWRule = domain.UFWRule
type UFWAllowRequest = domain.UFWAllowRequest
type AgentCertificateBundle = domain.AgentCertificateBundle
type SystemCertificate = domain.SystemCertificate

func Target(srv Server) sshx.Target {
	return sshx.Target{ServerID: srv.ID, Host: srv.Host, Port: srv.Port, Username: srv.SSHUsername, CredentialID: srv.CredentialID, PrivilegeMode: privilegeMode(srv)}
}

func hasPrivilege(srv Server) bool {
	return srv.Privilege.Privileged || srv.Privilege.Mode == sshx.PrivilegeModeRoot ||
		srv.Privilege.Mode == sshx.PrivilegeModeSudo || srv.Sudo.Passwordless
}

func privilegeMode(srv Server) string {
	switch srv.Privilege.Mode {
	case sshx.PrivilegeModeRoot, sshx.PrivilegeModeSudo, sshx.PrivilegeModeNone:
		return srv.Privilege.Mode
	default:
		if srv.Sudo.Passwordless {
			return sshx.PrivilegeModeSudo
		}
		return sshx.PrivilegeModeNone
	}
}
