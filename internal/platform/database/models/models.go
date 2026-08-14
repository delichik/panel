package models

// AllModels 返回四库全部存量表对应的模型注册清单，
// 父表先于子表排列，保证跨库建表时外键目标已存在。
func AllModels() []any {
	return append(append(append(append([]any{}, AppModels()...), LogModels()...), CoordinationModels()...), MetricsModels()...)
}

// AppModels 返回 app 库 33 张表的模型注册清单。
// 顺序满足 FK 依赖：父表（credentials、servers、applications、dns_domains、
// 各 edit_sessions）先于引用它们的子表。
func AppModels() []any {
	return []any{
		&Credential{}, &Server{}, &PanelInstallation{}, &PackageUpdate{}, &PackageRefresh{},
		&Fail2banConfig{}, &ImageUpdate{}, &ImageRefresh{}, &Application{},
		&ApplicationReconcileState{}, &ContainerObservation{}, &DockerResourceSnapshot{},
		&DNSDomain{}, &DNSRecordSnapshot{}, &ApplicationEditSession{},
		&ApplicationEditSessionFile{}, &ApplicationEditSessionOperation{},
		&ApplicationFile{}, &ApplicationInstance{}, &FacilityAppConfig{},
		&FacilityStaticAsset{}, &FacilityEditSession{}, &FacilityEditSessionAsset{},
		&FacilityEditSessionOperation{}, &StorageShareConfig{}, &StorageSharePartition{}, &Certificate{}, &SelfSignedCertificate{},
		&KeyAsset{}, &OverviewCardConfiguration{}, &RuntimeSetting{}, &AuthState{},
		&AuthAccount{},
	}
}

// LogModels 返回 log 库 7 张表的模型注册清单（事件与任务日志）。
func LogModels() []any {
	return []any{
		&Task{}, &TaskStep{}, &TaskLog{}, &ApplicationRevision{},
		&RuntimeEvent{}, &RuntimeEventDetail{}, &KeyAssetExport{},
	}
}

// CoordinationModels 返回协调库 3 张表的模型注册清单。
func CoordinationModels() []any {
	return []any{
		&ApplicationLifecycleOperation{}, &ApplicationLifecycleTarget{}, &ApplicationTargetStage{},
	}
}

// MetricsModels 返回 metrics 库 1 张表的模型注册清单。
func MetricsModels() []any {
	return []any{&MetricsSnapshot{}}
}

// ExtraIndexDDLFor 汇总给定模型清单通过 ExtraIndexDDL() 声明的复合/部分/
// 复合 UNIQUE 索引 DDL，按表名分组。语句本身包含 IF NOT EXISTS，供
// Store.Migrate 在步骤之后幂等创建。
func ExtraIndexDDLFor(models []any) map[string][]string {
	out := map[string][]string{}
	for _, m := range models {
		if d, ok := m.(interface{ ExtraIndexDDL() map[string][]string }); ok {
			for table, ddlList := range d.ExtraIndexDDL() {
				out[table] = append(out[table], ddlList...)
			}
		}
	}
	return out
}
