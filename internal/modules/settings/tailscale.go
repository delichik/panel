package settings

import (
	"context"
	"strings"

	panelerr "panel/internal/platform/errors"
	"panel/internal/platform/tailscale"
)

// RuntimeTailscaleSettings 是对外暴露的 tailscale 配置与容器内实际态。
// 认证密钥只以 authKeyConfigured 的形式暴露，绝不回显。
type RuntimeTailscaleSettings struct {
	// authKey 是只写密钥：字段未导出，因此永远不会被序列化到任何响应。
	authKey           string
	AuthKeyConfigured bool                           `json:"authKeyConfigured"`
	Tags              []string                       `json:"tags"`
	Container         RuntimeTailscaleContainerState `json:"container"`
}

// RuntimeTailscaleContainerState 是 Panel 容器内 tailscaled 的实际态。
type RuntimeTailscaleContainerState struct {
	Available    bool   `json:"available"`
	Running      bool   `json:"running"`
	LoggedIn     bool   `json:"loggedIn"`
	Hostname     string `json:"hostname"`
	IPv4         string `json:"ipv4"`
	IPv6         string `json:"ipv6"`
	Version      string `json:"version"`
	BackendState string `json:"backendState"`
	LastError    string `json:"lastError"`
	UpdatedAt    string `json:"updatedAt"`
}

// RuntimeTailscaleUpdate 是设置写入时的 tailscale 分组。AuthKey 为只写字段：
// 空值表示保留既有密钥；显式清除必须走 clearAuthKey。
type RuntimeTailscaleUpdate struct {
	AuthKey      string   `json:"authKey"`
	ClearAuthKey bool     `json:"clearAuthKey"`
	Tags         []string `json:"tags"`
}

// TailscaleNodeCredentials 返回节点加入 tailnet 所需的认证密钥与 tag。
// 认证密钥只用于节点侧的一次性加入，不写入节点磁盘。
func (s *Service) TailscaleNodeCredentials() (string, []string) {
	rt := s.Runtime()
	return rt.Tailscale.authKey, append([]string(nil), rt.Tailscale.Tags...)
}

// RefreshTailscaleContainer 读取容器内 tailscale 实际态并更新进程级可达性。
// Panel 自身的 tailnet 可达性是“是否改用 tailnet 地址”的前置条件，因此每次
// 读取都必须同步该标志。
func (s *Service) RefreshTailscaleContainer(ctx context.Context) RuntimeTailscaleContainerState {
	state := s.readTailscaleContainer(ctx)
	tailscale.SetPanelReady(state.Available && state.Running && state.LoggedIn)
	return state
}

// ApplyTailscaleContainer 重新生成期望态配置并请求 panel-init 收敛。
func (s *Service) ApplyTailscaleContainer(ctx context.Context) (RuntimeTailscaleContainerState, error) {
	if err := s.writeTailscaleConfig(); err != nil {
		return s.storeTailscaleContainer(RuntimeTailscaleContainerState{}, err), err
	}
	if !s.tailscaleControl.Supported() {
		state := s.storeTailscaleContainer(RuntimeTailscaleContainerState{}, nil)
		tailscale.SetPanelReady(false)
		return state, panelerr.Validation("tailscale_container_unavailable", "Panel is not supervised by panel-init; container tailscale cannot be managed")
	}
	if _, err := s.tailscaleControl.Apply(ctx); err != nil {
		return s.storeTailscaleContainer(RuntimeTailscaleContainerState{}, err), err
	}
	return s.RefreshTailscaleContainer(ctx), nil
}

// ensureTailscaleConfig 在启动时把持久化设置写回期望态配置文件，使容器重启后
// panel-init 能在 Panel 进程启动之前按同一份配置拉起 tailscaled。
func (s *Service) ensureTailscaleConfig() error {
	return s.writeTailscaleConfig()
}

func (s *Service) readTailscaleContainer(ctx context.Context) RuntimeTailscaleContainerState {
	if !s.tailscaleControl.Supported() {
		// 独立运行 panel（未经 panel-init 监管）时不存在容器内 tailscale，
		// 这不是错误：对外只表达“当前部署无法管理”。
		return s.storeTailscaleContainer(RuntimeTailscaleContainerState{}, nil)
	}
	status, err := s.tailscaleControl.Status(ctx)
	if err != nil {
		return s.storeTailscaleContainer(RuntimeTailscaleContainerState{Available: true}, err)
	}
	return s.storeTailscaleContainer(tailscaleContainerFromStatus(status), nil)
}

func (s *Service) storeTailscaleContainer(state RuntimeTailscaleContainerState, cause error) RuntimeTailscaleContainerState {
	if cause != nil {
		state.LastError = cause.Error()
	}
	s.mu.Lock()
	s.rt.Tailscale.Container = state
	s.mu.Unlock()
	return state
}

func tailscaleContainerFromStatus(status tailscale.Status) RuntimeTailscaleContainerState {
	state := RuntimeTailscaleContainerState{
		Available:    status.Available,
		Running:      status.Running,
		LoggedIn:     status.LoggedIn,
		Hostname:     strings.TrimSpace(status.Hostname),
		IPv4:         strings.TrimSpace(status.IPv4),
		IPv6:         strings.TrimSpace(status.IPv6),
		Version:      strings.TrimSpace(status.Version),
		BackendState: strings.TrimSpace(status.BackendState),
		LastError:    strings.TrimSpace(status.LastError),
	}
	if !status.UpdatedAt.IsZero() {
		state.UpdatedAt = status.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return state
}

// writeTailscaleConfig 把当前设置写为 panel-init 的期望态。未配置认证密钥时
// 期望态为“关闭”，panel-init 会停止容器内 tailscaled。
func (s *Service) writeTailscaleConfig() error {
	rt := s.Runtime()
	cfg := tailscale.Config{
		Enabled:  strings.TrimSpace(rt.Tailscale.authKey) != "",
		AuthKey:  rt.Tailscale.authKey,
		Tags:     append([]string(nil), rt.Tailscale.Tags...),
		Hostname: tailscale.PanelHostname,
	}
	if err := tailscale.WriteConfig(s.cfg.DataRoot, cfg); err != nil {
		return panelerr.Validation("tailscale_config_write_failed", "Failed to persist the container tailscale configuration: "+err.Error())
	}
	return nil
}

// applyTailscaleUpdate 校验并持久化设置页提交的 tailscale 分组。
//
// 语义与既有的秘密字段一致：空 authKey 表示保留旧密钥，显式清除必须提供
// clearAuthKey，避免一次普通的标签保存就把密钥清空。
func (s *Service) applyTailscaleUpdate(input RuntimeTailscaleUpdate) error {
	tags, err := normalizeTailscaleTags(input.Tags)
	if err != nil {
		return err
	}
	current := s.Runtime()
	nextKey := current.Tailscale.authKey
	authKeyConfigured := current.Tailscale.AuthKeyConfigured
	switch {
	case strings.TrimSpace(input.AuthKey) != "":
		key := strings.TrimSpace(input.AuthKey)
		if err := tailscale.ValidateAuthKey(key); err != nil {
			return panelerr.Validation("invalid_tailscale_auth_key", "Tailscale auth key must start with tskey- and contain no spaces")
		}
		nextKey = key
		authKeyConfigured = true
	case input.ClearAuthKey:
		nextKey = ""
		authKeyConfigured = false
	}
	values := map[string]string{RuntimeSettingTailscaleTags: encodeStringList(tags)}
	if nextKey != current.Tailscale.authKey {
		values[RuntimeSettingTailscaleAuthKey] = nextKey
	}
	// 使用脱离请求取消的上下文：设置写入是单事务，已经到达服务端的保存不应
	// 因为客户端断线而半途失败；失败时下面的内存更新不会执行，仍满足
	// SET-RUN-005 的“不得出现数据库已改、内存未改”。
	if err := s.saveValues(context.WithoutCancel(context.Background()), values); err != nil {
		return err
	}
	s.mu.Lock()
	s.rt.Tailscale.authKey = nextKey
	s.rt.Tailscale.AuthKeyConfigured = authKeyConfigured
	s.rt.Tailscale.Tags = tags
	s.mu.Unlock()
	return nil
}

// syncTailscaleConfig 在设置保存后把期望态下发给 panel-init，并刷新实际态。
// 下发失败不使设置保存失败：配置已持久化，实际态会如实显示失败原因并可由
// 设置页的“重新应用”重试。
func (s *Service) syncTailscaleConfig(ctx context.Context) {
	if err := s.writeTailscaleConfig(); err != nil {
		s.storeTailscaleContainer(RuntimeTailscaleContainerState{}, err)
		return
	}
	if !s.tailscaleControl.Supported() {
		s.storeTailscaleContainer(RuntimeTailscaleContainerState{}, nil)
		tailscale.SetPanelReady(false)
		return
	}
	if _, err := s.tailscaleControl.Apply(ctx); err != nil {
		s.storeTailscaleContainer(RuntimeTailscaleContainerState{Available: true}, err)
		return
	}
	s.RefreshTailscaleContainer(ctx)
}

func normalizeTailscaleTags(tags []string) ([]string, error) {
	normalized, err := tailscale.NormalizeTags(tags)
	if err != nil {
		return nil, panelerr.Validation("invalid_tailscale_tag", "Tailscale tags must look like tag:name, use lowercase letters, digits and hyphens")
	}
	if normalized == nil {
		normalized = []string{}
	}
	return normalized, nil
}

// encodeStringList 以换行分隔持久化字符串列表，读取时忽略空项。
func encodeStringList(values []string) string {
	return strings.Join(values, "\n")
}

func decodeStringList(value string) []string {
	out := []string{}
	for _, item := range strings.Split(value, "\n") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
