package tasks

func RegisterKnownTaskTypes(s *Service) {
	for _, def := range []Definition{
		{Type: "task_queue_drain", Hidden: true, ConcurrencyPolicy: ConcurrencyGlobalExclusive},
		{Type: "server_agent_check", Hidden: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "server_connectivity_test", Hidden: true, AllowRunNow: true, AllowRetry: true, DefaultMaxRetries: 8, ConcurrencyPolicy: ConcurrencyCustomKey, ConcurrencyKey: serverConnectivityConcurrencyKey},
		{Type: "server_info_collect", AllowRunNow: true, AllowRetry: true, DefaultMaxRetries: 8, ConcurrencyPolicy: ConcurrencyCustomKey, ConcurrencyKey: serverConnectivityConcurrencyKey},
		{Type: "server_ufw_install", AllowRetry: true},
		{Type: "server_ufw_enable", AllowRetry: true},
		{Type: "server_restart", AllowRetry: true},
		{Type: "server_agent_deploy", AllowRunNow: true, AllowRetry: true},
		{Type: "agent_certificate_reset", AllowRetry: true},
		{Type: "package_refresh", AllowRunNow: true, AllowRetry: true},
		{Type: "package_upgrade_selected", AllowRetry: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "package_upgrade_all", AllowRetry: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "metrics_collect", Hidden: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "certificate_issue", AllowRunNow: true, AllowRetry: true},
		{Type: "certificate_renew", AllowRetry: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "certificate_self_signed_renew", AllowRetry: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "application_deploy", AllowRetry: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "application_stop", AllowRetry: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "application_restart", AllowRetry: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "application_refresh", AllowRetry: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "application_image_check", ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "application_image_update", AllowRetry: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "container_refresh", AllowRetry: true},
		{Type: "image_refresh", AllowRetry: true},
		{Type: "volume_refresh", AllowRetry: true},
		{Type: "application_reconcile", AllowRetry: true, DefaultMaxRetries: 3},
		{Type: "application_image_upgrade_selected", AllowRetry: true, ConcurrencyPolicy: ConcurrencyGlobalExclusive},
		{Type: "application_image_upgrade_all", AllowRetry: true, ConcurrencyPolicy: ConcurrencyGlobalExclusive},
		{Type: "key_asset_tls_reissue", AllowRetry: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "key_asset_ssh_regenerate", AllowRetry: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "key_asset_export", AllowRetry: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "key_asset_import", AllowRetry: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
		{Type: "key_asset_sync", AllowRetry: true, ConcurrencyPolicy: ConcurrencyParallelAllowed},
	} {
		if _, exists := s.Registry().Definition(def.Type); exists {
			continue
		}
		s.MustRegister(def)
	}
}

func serverConnectivityConcurrencyKey(in CreateInput) string {
	serverID := firstNonEmpty(in.ResourceID, in.ServerID, in.NodeID)
	if serverID == "" {
		return ""
	}
	return "server_connectivity:" + serverID
}
