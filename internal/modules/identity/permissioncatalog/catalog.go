// Package permissioncatalog 持有 Panel 的权限目录（唯一权威来源）。
//
// 目录描述每个权限的分组、只读/读写模式、依赖关系与展示用 i18n key。
// API 与界面元素映射在后续阶段补充；角色、会话与凭证的权限解析都以本目录
// 的 key 集合为准。
package permissioncatalog

import "sort"

// Mode 表示权限是否只读。只读凭证只保留 read 权限。
type Mode string

const (
	ModeRead  Mode = "read"
	ModeWrite Mode = "write"
)

// Group 是权限在角色编辑器中的分组。
type Group struct {
	Key      string `json:"key"`
	LabelKey string `json:"labelKey"`
	Order    int    `json:"order"`
}

// UITarget 描述权限影响的界面元素。Actionable kinds must carry an anchor
// that also exists in the Vue source; nav/page kinds may omit it.
type UITarget struct {
	Page   string `json:"page"`
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Anchor string `json:"anchor,omitempty"`
	Effect string `json:"effect"`
}

// Permission 是目录中的一条权限。
type Permission struct {
	Key            string     `json:"key"`
	Group          string     `json:"group"`
	LabelKey       string     `json:"labelKey"`
	DescriptionKey string     `json:"descriptionKey"`
	Risk           string     `json:"risk"` // normal | sensitive | critical
	Mode           Mode       `json:"mode"`
	Requires       []string   `json:"requires,omitempty"`
	// Grantable 为 false 时只有内置管理员可持有，不能分配给自定义角色。
	Grantable bool       `json:"grantable"`
	UI        []UITarget `json:"ui"`
}

var groups = []Group{
	{Key: "overview", LabelKey: "permissions.group.overview", Order: 10},
	{Key: "servers", LabelKey: "permissions.group.servers", Order: 20},
	{Key: "containers", LabelKey: "permissions.group.containers", Order: 30},
	{Key: "applications", LabelKey: "permissions.group.applications", Order: 40},
	{Key: "facilities", LabelKey: "permissions.group.facilities", Order: 50},
	{Key: "dns", LabelKey: "permissions.group.dns", Order: 60},
	{Key: "certificates", LabelKey: "permissions.group.certificates", Order: 70},
	{Key: "keyassets", LabelKey: "permissions.group.keyassets", Order: 80},
	{Key: "events", LabelKey: "permissions.group.events", Order: 90},
	{Key: "tasks", LabelKey: "permissions.group.tasks", Order: 100},
	{Key: "backups", LabelKey: "permissions.group.backups", Order: 110},
	{Key: "settings", LabelKey: "permissions.group.settings", Order: 120},
	{Key: "debug", LabelKey: "permissions.group.debug", Order: 130},
	{Key: "access", LabelKey: "permissions.group.access", Order: 140},
}

func p(key, group string, mode Mode, risk string, requires ...string) Permission {
	return Permission{
		Key:            key,
		Group:          group,
		LabelKey:       "permissions." + key + ".label",
		DescriptionKey: "permissions." + key + ".description",
		Risk:           risk,
		Mode:           mode,
		Requires:       requires,
		Grantable:      group != "access",
	}
}

var permissions = []Permission{
	p("overview.view", "overview", ModeRead, "normal"),

	p("servers.view", "servers", ModeRead, "normal"),
	p("servers.manage", "servers", ModeWrite, "sensitive", "servers.view"),
	p("servers.execute", "servers", ModeWrite, "sensitive", "servers.view"),
	p("servers.credentials.manage", "servers", ModeWrite, "critical", "servers.manage"),

	p("containers.view", "containers", ModeRead, "normal", "servers.view"),
	p("containers.manage", "containers", ModeWrite, "sensitive", "servers.manage", "containers.view"),

	p("applications.view", "applications", ModeRead, "normal", "servers.view"),
	p("applications.edit", "applications", ModeWrite, "sensitive", "applications.view", "servers.manage"),
	p("applications.deploy", "applications", ModeWrite, "sensitive", "applications.edit", "servers.execute"),

	p("facilities.view", "facilities", ModeRead, "normal", "servers.view"),
	p("facilities.manage", "facilities", ModeWrite, "sensitive", "facilities.view", "servers.manage"),

	p("dns.view", "dns", ModeRead, "normal"),
	p("dns.manage", "dns", ModeWrite, "sensitive", "dns.view"),

	p("certificates.view", "certificates", ModeRead, "normal"),
	p("certificates.manage", "certificates", ModeWrite, "sensitive", "certificates.view", "dns.view"),

	p("keyassets.view", "keyassets", ModeRead, "normal"),
	p("keyassets.manage", "keyassets", ModeWrite, "critical", "keyassets.view"),
	p("keyassets.export", "keyassets", ModeRead, "critical", "keyassets.view"),

	p("events.view", "events", ModeRead, "normal"),
	p("events.export", "events", ModeRead, "sensitive", "events.view"),
	p("events.manage", "events", ModeWrite, "critical", "events.view"),

	p("tasks.view", "tasks", ModeRead, "normal"),
	p("tasks.manage", "tasks", ModeWrite, "sensitive", "tasks.view"),

	p("backups.export", "backups", ModeRead, "critical"),
	p("backups.restore", "backups", ModeWrite, "critical", "backups.export"),

	p("settings.view", "settings", ModeRead, "normal"),
	p("settings.manage", "settings", ModeWrite, "sensitive", "settings.view"),
	p("settings.security", "settings", ModeWrite, "critical", "settings.view"),
	p("settings.appearance", "settings", ModeWrite, "normal", "settings.view"),

	p("debug.view", "debug", ModeRead, "sensitive"),
	p("debug.manage", "debug", ModeWrite, "critical", "debug.view"),

	p("access.users.manage", "access", ModeWrite, "critical"),
	p("access.roles.manage", "access", ModeWrite, "critical"),
	p("access.credentials.manage", "access", ModeWrite, "critical"),
}

var uiTargets = map[string][]UITarget{
	"overview.view":               {{Page: "overview", Kind: "nav", Target: "layout.nav.overview", Effect: "hide"}},
	"servers.view":                {{Page: "servers", Kind: "nav", Target: "layout.nav.servers", Effect: "hide"}},
	"servers.manage":              {{Page: "servers", Kind: "button", Target: "common.add", Anchor: "servers.manage.create", Effect: "hide"}},
	"servers.execute":             {{Page: "servers", Kind: "button", Target: "serversPage.restart", Anchor: "servers.execute.restart", Effect: "hide"}},
	"servers.credentials.manage":  {{Page: "credentials", Kind: "button", Target: "common.add", Anchor: "credentials.manage.create", Effect: "hide"}},
	"containers.view":             {{Page: "servers", Kind: "section", Target: "serversPage.tab.resources", Effect: "hide"}},
	"containers.manage":           {{Page: "servers", Kind: "action", Target: "containers.manage", Effect: "hide"}},
	"applications.view":           {{Page: "applications", Kind: "nav", Target: "layout.nav.applications", Effect: "hide"}},
	"applications.edit":           {{Page: "applications", Kind: "button", Target: "common.create", Anchor: "applications.edit.create", Effect: "hide"}},
	"applications.deploy":         {{Page: "applications", Kind: "button", Target: "applicationsPage.sync", Anchor: "applications.deploy.sync", Effect: "hide"}},
	"facilities.view":             {{Page: "facilities", Kind: "nav", Target: "layout.nav.facilityApps", Effect: "hide"}},
	"facilities.manage":           {{Page: "facilities", Kind: "button", Target: "common.addPath", Anchor: "facilities.manage.pathAdd", Effect: "hide"}},
	"dns.view":                    {{Page: "dns", Kind: "nav", Target: "layout.nav.dns", Effect: "hide"}},
	"dns.manage":                  {{Page: "dns", Kind: "button", Target: "dnsPage.addRecord", Anchor: "dns.manage.recordCreate", Effect: "hide"}},
	"certificates.view":           {{Page: "certificates", Kind: "nav", Target: "layout.nav.certificates", Effect: "hide"}},
	"certificates.manage":         {{Page: "certificates", Kind: "button", Target: "certificatesPage.issue", Anchor: "certificates.manage.issue", Effect: "hide"}},
	"keyassets.view":              {{Page: "certificates", Kind: "section", Target: "certificatesPage.keys", Effect: "hide"}},
	"keyassets.manage":            {{Page: "certificates", Kind: "button", Target: "certificatesPage.generateSsh", Anchor: "keyassets.manage.generate", Effect: "hide"}},
	"keyassets.export":            {{Page: "certificates", Kind: "menu", Target: "certificatesPage.exportAsset", Anchor: "keyassets.export.download", Effect: "hide"}},
	"events.view":                 {{Page: "events", Kind: "nav", Target: "layout.nav.events", Effect: "hide"}},
	"events.export":               {{Page: "events", Kind: "button", Target: "activity.export", Anchor: "events.export.download", Effect: "hide"}},
	"events.manage":               {{Page: "settings", Kind: "button", Target: "settingsPage.clearEventsLogs", Anchor: "events.manage.clear", Effect: "hide"}},
	"tasks.view":                  {{Page: "events", Kind: "page", Target: "tasks.view", Effect: "hide"}},
	"tasks.manage":                {{Page: "events", Kind: "action", Target: "tasks.manage", Effect: "hide"}},
	"backups.export":              {{Page: "settings", Kind: "action", Target: "settingsPage.startExport", Effect: "hide"}},
	"backups.restore":             {{Page: "settings", Kind: "action", Target: "settingsPage.restore", Effect: "hide"}},
	"settings.view":               {{Page: "settings", Kind: "nav", Target: "layout.nav.settings", Effect: "hide"}},
	"settings.manage":             {{Page: "settings", Kind: "field", Target: "settingsPage.remoteTimeout", Anchor: "settings.manage.remoteTimeout", Effect: "readonly"}},
	"settings.security":           {{Page: "settings", Kind: "button", Target: "settingsPage.resetJwtSecret", Anchor: "settings.security.jwtReset", Effect: "hide"}},
	"settings.appearance":         {{Page: "settings", Kind: "section", Target: "settingsPage.section.appearance", Effect: "hide"}},
	"debug.view":                  {{Page: "debug", Kind: "nav", Target: "routes.debug.title", Effect: "hide"}},
	"debug.manage":                {{Page: "debug", Kind: "button", Target: "debugPage.clearRuntimeData", Anchor: "debug.manage.clear", Effect: "hide"}},
	"access.users.manage":         {{Page: "access", Kind: "nav", Target: "routes.access.title", Effect: "hide"}},
	"access.roles.manage":         {{Page: "access", Kind: "section", Target: "access.roles.tab", Effect: "hide"}},
	"access.credentials.manage":   {{Page: "access", Kind: "action", Target: "access.credentials.manage", Effect: "hide"}},
}

var (
	byKey      map[string]Permission
	allKeys    []string
	readOnly   map[string]struct{}
	adminOnly  map[string]struct{}
	groupOrder = func() map[string]int {
		out := make(map[string]int, len(groups))
		for _, g := range groups {
			out[g.Key] = g.Order
		}
		return out
	}()
)

func init() {
	byKey = make(map[string]Permission, len(permissions))
	allKeys = make([]string, 0, len(permissions))
	readOnly = map[string]struct{}{}
	adminOnly = map[string]struct{}{}
	for i := range permissions {
		permissions[i].UI = uiTargets[permissions[i].Key]
		if len(permissions[i].UI) == 0 {
			permissions[i].UI = []UITarget{{Page: permissions[i].Group, Kind: "page", Target: permissions[i].Key, Effect: "hide"}}
		}
		perm := permissions[i]
		byKey[perm.Key] = perm
		allKeys = append(allKeys, perm.Key)
		if perm.Mode == ModeRead {
			readOnly[perm.Key] = struct{}{}
		}
		if !perm.Grantable {
			adminOnly[perm.Key] = struct{}{}
		}
	}
	sort.Strings(allKeys)
}

// Groups 返回分组定义。
func Groups() []Group { return append([]Group(nil), groups...) }

// Permissions 返回全部权限（按分组顺序和 key 排序）。
func Permissions() []Permission {
	out := append([]Permission(nil), permissions...)
	sort.SliceStable(out, func(i, j int) bool {
		gi, gj := groupOrder[out[i].Group], groupOrder[out[j].Group]
		if gi != gj {
			return gi < gj
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// AllKeys 返回全部权限 key（排序后）。
func AllKeys() []string { return append([]string(nil), allKeys...) }

// Lookup 返回权限定义。
func Lookup(key string) (Permission, bool) {
	perm, ok := byKey[key]
	return perm, ok
}

// IsReadOnly 报告权限是否属于只读模式。
func IsReadOnly(key string) bool {
	_, ok := readOnly[key]
	return ok
}

// IsAdminOnly 报告权限是否只能由内置管理员持有。
func IsAdminOnly(key string) bool {
	_, ok := adminOnly[key]
	return ok
}

// LookupGrantable 报告权限是否存在且可分配给自定义角色。
func LookupGrantable(key string) bool {
	perm, ok := byKey[key]
	return ok && perm.Grantable
}

// Expand 返回包含依赖闭包的权限集合；未知 key 或依赖不合法时返回 false。
func Expand(keys []string) ([]string, bool) {
	seen := map[string]struct{}{}
	var visit func(string) bool
	visit = func(key string) bool {
		if _, done := seen[key]; done {
			return true
		}
		perm, ok := byKey[key]
		if !ok {
			return false
		}
		seen[key] = struct{}{}
		for _, req := range perm.Requires {
			if !visit(req) {
				return false
			}
		}
		return true
	}
	for _, key := range keys {
		if !visit(key) {
			return nil, false
		}
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out, true
}

// FilterReadOnly 返回只保留只读权限后的集合（保持输入顺序去重）。
func FilterReadOnly(keys []string) []string {
	out := make([]string, 0, len(keys))
	seen := map[string]struct{}{}
	for _, key := range keys {
		if !IsReadOnly(key) {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}
