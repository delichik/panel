package server

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	agentcontract "panel/internal/agent/contract"
	"panel/internal/modules/tasks"
	"panel/internal/platform/database/models"
	"panel/internal/platform/database/orm"
	panelerr "panel/internal/platform/errors"

	"gopkg.in/yaml.v3"
)

var fail2banNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func (s *Service) Fail2BanState(ctx context.Context, serverID string) (Fail2BanState, error) {
	srv, err := s.ensureFail2BanManageable(ctx, serverID)
	if err != nil {
		return Fail2BanState{}, err
	}
	configYAML, managed, updatedAt, err := s.loadFail2BanConfig(ctx, serverID)
	if err != nil {
		return Fail2BanState{}, err
	}
	config, normalizedYAML, err := parseFail2BanYAML(configYAML)
	if err != nil {
		return Fail2BanState{}, err
	}
	state := Fail2BanState{ServerID: serverID, Managed: managed, ConfigYAML: normalizedYAML, Config: config, UpdatedAt: updatedAt, Jails: []string{}}
	maintenance, baseURL, ok, err := s.agentMaintenance(srv)
	if err != nil {
		return Fail2BanState{}, err
	}
	if !ok {
		return state, nil
	}
	status, err := maintenance.Fail2BanStatus(ctx, baseURL)
	if err != nil {
		_ = s.handleAgentCertificateTimeError(ctx, srv, err)
		return Fail2BanState{}, err
	}
	state.Installed = status.Installed
	state.Active = status.Active
	state.PanelConfigPresent = status.PanelConfigPresent
	state.Jails = status.Jails
	state.Raw = status.Raw
	return state, nil
}

func (s *Service) SaveFail2Ban(ctx context.Context, serverID string, req Fail2BanUpdateRequest) (Fail2BanState, error) {
	config, normalizedYAML, err := parseFail2BanYAML(req.ConfigYAML)
	if err != nil {
		return Fail2BanState{}, err
	}
	_ = config
	if err := s.saveFail2BanConfig(ctx, serverID, normalizedYAML, nil); err != nil {
		return Fail2BanState{}, err
	}
	return s.Fail2BanState(ctx, serverID)
}

func (s *Service) EnableFail2Ban(ctx context.Context, serverID string, req Fail2BanEnableRequest) (tasks.Task, error) {
	configYAML := req.ConfigYAML
	if strings.TrimSpace(configYAML) == "" {
		loaded, _, _, err := s.loadFail2BanConfig(ctx, serverID)
		if err != nil {
			return tasks.Task{}, err
		}
		configYAML = loaded
	}
	config, normalizedYAML, err := parseFail2BanYAML(configYAML)
	if err != nil {
		return tasks.Task{}, err
	}
	state, err := s.Fail2BanState(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	if state.Installed && !state.Managed && !req.ConfirmTakeover {
		return tasks.Task{}, panelerr.Validation("fail2ban_takeover_confirmation_required", "Taking over an installed fail2ban service requires confirmation")
	}
	return s.createFail2BanApplyTask(ctx, serverID, config, normalizedYAML, "Applying fail2ban protection rules")
}

func (s *Service) InstallFail2Ban(ctx context.Context, serverID string) (tasks.Task, error) {
	return s.EnableFail2Ban(ctx, serverID, Fail2BanEnableRequest{ConfirmTakeover: true})
}

func (s *Service) ReleaseFail2Ban(ctx context.Context, serverID string) (tasks.Task, error) {
	return s.createFail2BanReleaseTask(ctx, serverID, "Releasing fail2ban management")
}

func (s *Service) createFail2BanApplyTask(ctx context.Context, serverID string, config Fail2BanConfig, configYAML, summary string) (tasks.Task, error) {
	srv, err := s.ensureFail2BanManageable(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		Type:         fail2banApplyTaskType,
		ServerID:     serverID,
		ResourceType: connectivityResourceType,
		ResourceID:   serverID,
		TriggerType:  "user",
		Summary:      summary,
		MaxRetries:   0,
	}, tasks.Trigger{Type: "user", Manual: true})
	if err != nil {
		return tasks.Task{}, err
	}
	if !created {
		return task, nil
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return tasks.Task{}, err
	}
	task, err = s.tasks.Get(ctx, task.ID)
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runApplyFail2Ban(s.tasks.ExecutionContext(task.ID), task.ID, srv, config, configYAML)
	return task, nil
}

func (s *Service) createFail2BanReleaseTask(ctx context.Context, serverID string, summary string) (tasks.Task, error) {
	srv, err := s.ensureFail2BanManageable(ctx, serverID)
	if err != nil {
		return tasks.Task{}, err
	}
	task, created, err := tasks.NewManager(s.tasks).Create(ctx, tasks.CreateInput{
		Type:         fail2banApplyTaskType,
		ServerID:     serverID,
		ResourceType: connectivityResourceType,
		ResourceID:   serverID,
		TriggerType:  "user",
		Summary:      summary,
		MaxRetries:   0,
	}, tasks.Trigger{Type: "user", Manual: true})
	if err != nil {
		return tasks.Task{}, err
	}
	if !created {
		return task, nil
	}
	if err := s.tasks.Start(ctx, task.ID); err != nil {
		return tasks.Task{}, err
	}
	task, err = s.tasks.Get(ctx, task.ID)
	if err != nil {
		return tasks.Task{}, err
	}
	go s.runReleaseFail2Ban(s.tasks.ExecutionContext(task.ID), task.ID, srv)
	return task, nil
}

func (s *Service) runApplyFail2Ban(ctx context.Context, taskID string, srv Server, config Fail2BanConfig, configYAML string) {
	defer s.tasks.FinishExecution(taskID)
	if err := ctx.Err(); err != nil {
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "validating", "validating fail2ban configuration")
	maintenance, baseURL, ok, err := s.agentMaintenance(srv)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	if !ok {
		_ = s.tasks.Fail(ctx, taskID, panelerr.Validation("agent_required", "Agent is required for fail2ban configuration"))
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "applying", "applying fail2ban configuration")
	if _, err := maintenance.ApplyFail2Ban(ctx, baseURL, agentcontract.Fail2BanApplyRequest{Config: fail2BanConfigToAgent(config)}); err != nil {
		_ = s.handleAgentCertificateTimeError(ctx, srv, err)
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	managed := true
	if err := s.saveFail2BanConfig(ctx, srv.ID, configYAML, &managed); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Complete(ctx, taskID, "fail2ban configuration applied")
}

func (s *Service) runReleaseFail2Ban(ctx context.Context, taskID string, srv Server) {
	defer s.tasks.FinishExecution(taskID)
	if err := ctx.Err(); err != nil {
		return
	}
	_ = s.tasks.Advance(ctx, taskID, "releasing", "releasing fail2ban management")
	maintenance, baseURL, ok, err := s.agentMaintenance(srv)
	if err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	if !ok {
		_ = s.tasks.Fail(ctx, taskID, panelerr.Validation("agent_required", "Agent is required for fail2ban configuration"))
		return
	}
	if _, err := maintenance.ReleaseFail2Ban(ctx, baseURL); err != nil {
		_ = s.handleAgentCertificateTimeError(ctx, srv, err)
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	managed := false
	if err := s.saveFail2BanManaged(ctx, srv.ID, managed); err != nil {
		_ = s.tasks.Fail(ctx, taskID, err)
		return
	}
	_ = s.tasks.Complete(ctx, taskID, "fail2ban management released")
}

func (s *Service) ensureFail2BanManageable(ctx context.Context, serverID string) (Server, error) {
	srv, err := s.Get(ctx, serverID)
	if err != nil {
		return Server{}, err
	}
	if !srv.OS.Supported {
		return Server{}, panelerr.Validation("server_not_supported", "Server distribution is not supported")
	}
	if !srv.Reachable {
		return Server{}, panelerr.Validation("server_not_reachable", "Server connectivity has not been confirmed")
	}
	if !hasPrivilege(srv) {
		return Server{}, panelerr.Validation("privileged_access_required", "Root or passwordless sudo access is required")
	}
	return srv, nil
}

func (s *Service) loadFail2BanConfig(ctx context.Context, serverID string) (string, bool, *time.Time, error) {
	var cfg models.Fail2banConfig
	err := orm.New(s.db).From("fail2ban_configs").Where("server_id=?", serverID).First(ctx, &cfg)
	if err == nil {
		if cfg.UpdatedAt.IsZero() {
			return cfg.ConfigYAML, cfg.Managed, nil, nil
		}
		return cfg.ConfigYAML, cfg.Managed, &cfg.UpdatedAt, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return defaultFail2BanYAML(), false, nil, nil
	}
	return "", false, nil, err
}

func (s *Service) saveFail2BanConfig(ctx context.Context, serverID, configYAML string, managed *bool) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	managedValue := 0
	if managed != nil && *managed {
		managedValue = 1
	}
	if managed == nil {
		_, err := orm.RawExec(ctx, s.db, `INSERT INTO fail2ban_configs(server_id, config_yaml, updated_at) VALUES(?,?,?) ON CONFLICT(server_id) DO UPDATE SET config_yaml=excluded.config_yaml, updated_at=excluded.updated_at`, serverID, configYAML, now)
		return err
	}
	_, err := orm.RawExec(ctx, s.db, `INSERT INTO fail2ban_configs(server_id, config_yaml, managed, updated_at) VALUES(?,?,?,?) ON CONFLICT(server_id) DO UPDATE SET config_yaml=excluded.config_yaml, managed=excluded.managed, updated_at=excluded.updated_at`, serverID, configYAML, managedValue, now)
	return err
}

func (s *Service) saveFail2BanManaged(ctx context.Context, serverID string, managed bool) error {
	configYAML, _, _, err := s.loadFail2BanConfig(ctx, serverID)
	if err != nil {
		return err
	}
	return s.saveFail2BanConfig(ctx, serverID, configYAML, &managed)
}

func parseFail2BanYAML(raw string) (Fail2BanConfig, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = defaultFail2BanYAML()
	}
	decoder := yaml.NewDecoder(strings.NewReader(raw))
	decoder.KnownFields(true)
	var config Fail2BanConfig
	if err := decoder.Decode(&config); err != nil {
		return Fail2BanConfig{}, "", panelerr.Validation("fail2ban_config_invalid", "fail2ban YAML is invalid: "+err.Error())
	}
	if err := validateFail2BanConfig(config); err != nil {
		return Fail2BanConfig{}, "", err
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	encoder.SetIndent(2)
	if err := encoder.Encode(config); err != nil {
		return Fail2BanConfig{}, "", err
	}
	_ = encoder.Close()
	return config, strings.TrimSpace(out.String()) + "\n", nil
}

func validateFail2BanConfig(config Fail2BanConfig) error {
	if len(config.Jails) == 0 {
		return panelerr.Validation("fail2ban_jail_required", "At least one fail2ban jail is required")
	}
	seen := map[string]struct{}{}
	for _, jail := range config.Jails {
		name := strings.TrimSpace(jail.Name)
		if name == "" || !fail2banNamePattern.MatchString(name) {
			return panelerr.Validation("fail2ban_jail_name_invalid", "fail2ban jail name is invalid")
		}
		if _, exists := seen[name]; exists {
			return panelerr.Validation("fail2ban_jail_duplicate", "fail2ban jail name is duplicated")
		}
		seen[name] = struct{}{}
		if jail.MaxRetry < 0 {
			return panelerr.Validation("fail2ban_maxretry_invalid", "fail2ban maxretry cannot be negative")
		}
		for key, value := range fail2banJailValues(jail) {
			if strings.ContainsAny(value, "\r\n") {
				return panelerr.Validation("fail2ban_value_invalid", fmt.Sprintf("fail2ban %s must be a single line", key))
			}
		}
		for key, value := range jail.Options {
			if strings.TrimSpace(key) == "" || !fail2banNamePattern.MatchString(strings.TrimSpace(key)) {
				return panelerr.Validation("fail2ban_option_key_invalid", "fail2ban option key is invalid")
			}
			if strings.ContainsAny(value, "\r\n") {
				return panelerr.Validation("fail2ban_value_invalid", "fail2ban option values must be single-line strings")
			}
		}
	}
	return nil
}

func fail2banJailValues(jail Fail2BanJail) map[string]string {
	return map[string]string{
		"filter":   jail.Filter,
		"logpath":  jail.LogPath,
		"backend":  jail.Backend,
		"port":     jail.Port,
		"protocol": jail.Protocol,
		"action":   jail.Action,
		"findtime": jail.FindTime,
		"bantime":  jail.BanTime,
		"ignoreip": strings.Join(jail.IgnoreIP, " "),
	}
}

func defaultFail2BanYAML() string {
	return strings.TrimSpace(`jails:
  - name: sshd
    enabled: true
    preset: ssh
    filter: sshd
    port: ssh
    logpath: /var/log/auth.log
    backend: systemd
    maxretry: 5
    findtime: 10m
    bantime: 1h
    ignoreip:
      - 127.0.0.1/8
`) + "\n"
}

func fail2BanConfigToAgent(config Fail2BanConfig) agentcontract.Fail2BanConfig {
	jails := make([]agentcontract.Fail2BanJail, 0, len(config.Jails))
	for _, jail := range config.Jails {
		jails = append(jails, agentcontract.Fail2BanJail{
			Name:     jail.Name,
			Enabled:  jail.Enabled,
			Preset:   jail.Preset,
			Filter:   jail.Filter,
			LogPath:  jail.LogPath,
			Backend:  jail.Backend,
			Port:     jail.Port,
			Protocol: jail.Protocol,
			Action:   jail.Action,
			MaxRetry: jail.MaxRetry,
			FindTime: jail.FindTime,
			BanTime:  jail.BanTime,
			IgnoreIP: jail.IgnoreIP,
			Options:  jail.Options,
		})
	}
	return agentcontract.Fail2BanConfig{Jails: jails}
}
