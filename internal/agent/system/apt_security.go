package system

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"panel/internal/platform/linux"
)

// 安全更新判定完全基于系统自带的 apt 元数据/策略，不再读取包列表里的
// source 字符串，也不再把 unattended-upgrade 二进制的 dry-run 输出当作主判定：
//
//  1. Allowed-Origins（兼容 unattended-upgrades 1.x/2.x 的 Allowed-Origins、
//     Allowed-Origins::、Origins-Pattern 形式）定义哪些 origin 属于安全归档；
//  2. apt-cache policy 给出每个包 Candidate 版本的实际来源（suite/component 与
//     release o=...,a=...,n=...,l=... 行）；
//  3. 没有可用 Allowed-Origins 策略时退回 apt 元数据命名规则（suite/archive 以
//     -security 结尾，或官方安全站点 host + 安全 suite）。
//
// 命令失败、超时、Candidate 无可解析来源时只是“不可判定”（SecurityKnown=false），
// 绝不能把未知当成“非安全”。

const (
	// aptCachePolicyTimeout 限制单批 apt-cache policy 的耗时；超时按不可判定
	// 处理，不能拖慢或失败 PackageUpdates。
	aptCachePolicyTimeout = 10 * time.Second
	// aptCachePolicyMaxArgs 与 aptCachePolicyMaxArgChars 控制单次 apt-cache policy
	// 的包数量/参数长度，避免可升级列表很大时超过 ARG_MAX。
	aptCachePolicyMaxArgs     = 64
	aptCachePolicyMaxArgChars = 8000
)

// 这两个路径在测试中可替换，默认读取 Agent 所在主机的真实系统配置。
var (
	aptSecurityAllowedOriginsGlob = "/etc/apt/apt.conf.d/*unattended*"
	aptSecurityOSReleasePath      = "/etc/os-release"
)

// aptRelease 对应 apt-cache policy 的 release 行字段（release o=...,a=...,...）。
// 注意真实 apt-cache policy 通常不输出 release 行，此时由 source line 的
// suite/component 提供归档信息。
type aptRelease struct {
	Origin    string
	Archive   string
	Codename  string
	Label     string
	Component string
}

// aptSource 是 Candidate 版本的一个可用来源（apt-cache policy 的 source line）。
type aptSource struct {
	URL       string
	Suite     string
	Component string
	Release   aptRelease
}

// aptCandidate 是某个包 Candidate 版本的来源信息。
type aptCandidate struct {
	Version string
	Sources []aptSource
}

// securityInfo 是单个包的权威安全判定结果。Known=false 表示无法判定。
type securityInfo struct {
	Security bool
	Known    bool
}

// originPattern 是 unattended-upgrades 策略中的一条 origin 规则。
// 既支持 "Origin:Archive"（Ubuntu Allowed-Origins 风格），也支持
// "o=Origin,a=Archive,n=Codename,l=Label"（Debian Origins-Pattern 风格）。
type originPattern struct {
	Origin   string
	Archive  string
	Codename string
	Label    string
}

// applyAptSecurity 用 apt 元数据为可升级列表补充权威安全判定。它是纯尽力而为
// 的步骤：任何失败都只让对应包保持未知，绝不影响 PackageUpdates 本身。
func applyAptSecurity(ctx context.Context, updates []linux.PackageUpdate) {
	if len(updates) == 0 {
		return
	}
	osRelease := readOSRelease(aptSecurityOSReleasePath)
	origins := loadAllowedOrigins(defaultAllowedOriginsPaths(), osRelease)

	names := make([]string, 0, len(updates))
	for _, update := range updates {
		if packageNamePattern.MatchString(update.Name) {
			names = append(names, update.Name)
		}
	}
	for _, batch := range batchPackageNames(names) {
		args := append([]string{"policy"}, batch...)
		out, err := runCommand(ctx, aptCachePolicyTimeout, "apt-cache", args...)
		if err != nil {
			// 整批不可判定：保持 SecurityKnown=false，由 Panel 跳过而不是猜测。
			continue
		}
		infos := parseAptCachePolicySecurity(out, origins)
		for index := range updates {
			info, ok := infos[updates[index].Name]
			if !ok {
				continue
			}
			updates[index].Security = info.Security
			updates[index].SecurityKnown = info.Known
		}
	}
}

// batchPackageNames 按包数量与参数字符数分批，避免 apt-cache policy 命令行过长。
func batchPackageNames(names []string) [][]string {
	var batches [][]string
	var current []string
	chars := 0
	for _, name := range names {
		if len(current) >= aptCachePolicyMaxArgs || chars+len(name)+1 > aptCachePolicyMaxArgChars {
			batches = append(batches, current)
			current = nil
			chars = 0
		}
		current = append(current, name)
		chars += len(name) + 1
	}
	if len(current) > 0 {
		batches = append(batches, current)
	}
	return batches
}

// parseAptCachePolicySecurity 解析 apt-cache policy 输出并给出每个包 Candidate
// 版本的安全判定。纯函数，便于用系统真实格式样例做确定性单测。
func parseAptCachePolicySecurity(output string, origins []originPattern) map[string]securityInfo {
	candidates := parseAptCachePolicy(output)
	infos := make(map[string]securityInfo, len(candidates))
	for name, candidate := range candidates {
		infos[name] = classifyAptCandidate(candidate, origins)
	}
	return infos
}

// classifyAptCandidate 按 Candidate 版本的来源判定安全状态：
//   - 有 Allowed-Origins 安全策略时，origin/archive/suite/codename 命中即安全；
//   - 否则按 apt 元数据命名/官方安全站点判定；
//   - 没有任何可解析来源时不可判定。
func classifyAptCandidate(candidate aptCandidate, origins []originPattern) securityInfo {
	usable := false
	for _, source := range candidate.Sources {
		if !sourceUsable(source) {
			continue
		}
		usable = true
		if sourceMatchesSecurityOrigin(source, origins) {
			return securityInfo{Security: true, Known: true}
		}
	}
	if !usable {
		return securityInfo{}
	}
	return securityInfo{Known: true}
}

// sourceUsable 判断 source line 是否携带可用的归档信息。只有 url（例如
// /var/lib/dpkg/status 这类本地来源）而没有 suite/release 的来源不可用于判定。
func sourceUsable(source aptSource) bool {
	if source.Suite != "" && source.Suite != "." {
		return true
	}
	return source.Release.Archive != "" || source.Release.Codename != "" || source.Release.Label != ""
}

// sourceMatchesSecurityOrigin 是安全判定的唯一入口。origins 应只包含
// security 类允许 origin（见 securityOriginPatterns）；为空时使用命名规则回退。
func sourceMatchesSecurityOrigin(source aptSource, origins []originPattern) bool {
	if len(origins) > 0 {
		for _, pattern := range origins {
			if originPatternMatches(pattern, source) {
				return true
			}
		}
		return false
	}
	return aptMetadataSecurity(source)
}

// originPatternMatches 判断 Candidate 来源是否命中一条 Allowed-Origins 规则。
// 规则里出现的字段都必须匹配；候选来源缺少 origin 元数据时（apt-cache policy
// 不输出 release 行）只按 archive/suite/codename/label 匹配。
func originPatternMatches(pattern originPattern, source aptSource) bool {
	matched := false
	if pattern.Origin != "" && source.Release.Origin != "" {
		matched = true
		if !strings.EqualFold(pattern.Origin, source.Release.Origin) {
			return false
		}
	}
	if pattern.Archive != "" {
		matched = true
		if !sourceFieldMatches(pattern.Archive, source.Release.Archive, source.Release.Codename, source.Suite) {
			return false
		}
	}
	if pattern.Codename != "" {
		matched = true
		if !sourceFieldMatches(pattern.Codename, source.Release.Codename, source.Suite, source.Release.Archive) {
			return false
		}
	}
	if pattern.Label != "" {
		matched = true
		if !sourceFieldMatches(pattern.Label, source.Release.Label) {
			return false
		}
	}
	return matched
}

func sourceFieldMatches(pattern string, values ...string) bool {
	for _, value := range values {
		if value != "" && strings.EqualFold(pattern, value) {
			return true
		}
	}
	return false
}

// aptMetadataSecurity 是无 Allowed-Origins 策略时的回退规则：只依据 apt 元数据
// 中安全归档的命名（*-security）或官方安全站点 host + 安全 suite。绝不读取
// PackageUpdate.Source。
func aptMetadataSecurity(source aptSource) bool {
	for _, name := range []string{source.Release.Archive, source.Release.Codename, source.Release.Label, source.Suite} {
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(name)), "-security") {
			return true
		}
	}
	switch strings.ToLower(sourceHost(source.URL)) {
	case "security.ubuntu.com", "security.debian.org", "esm.ubuntu.com":
		for _, name := range []string{source.Release.Archive, source.Release.Codename, source.Release.Label, source.Suite} {
			if strings.Contains(strings.ToLower(name), "security") {
				return true
			}
		}
	}
	return false
}

func sourceHost(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Hostname()
}

// parseAptCachePolicy 解析 apt-cache policy 的纯文本输出，只保留每个包
// Candidate 版本的来源。多版本包只按 Candidate 判定，installed/其它版本不参与。
func parseAptCachePolicy(output string) map[string]aptCandidate {
	candidates := map[string]aptCandidate{}
	lines := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n")

	var pkg string
	var candidate string
	var currentVersion string
	var currentSources []aptSource
	var currentRelease aptRelease
	var inVersionTable bool

	storeCandidate := func() {
		if pkg == "" || candidate == "" || candidate == "(none)" || currentVersion != candidate || len(currentSources) == 0 {
			return
		}
		candidates[pkg] = aptCandidate{Version: candidate, Sources: append([]aptSource(nil), currentSources...)}
	}
	startVersion := func(version string) {
		storeCandidate()
		currentVersion = version
		currentSources = nil
		currentRelease = aptRelease{}
	}
	finishPackage := func() {
		storeCandidate()
		pkg = ""
		candidate = ""
		currentVersion = ""
		currentSources = nil
		currentRelease = aptRelease{}
		inVersionTable = false
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indented := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
		if !indented && strings.HasSuffix(trimmed, ":") {
			finishPackage()
			pkg = strings.TrimSuffix(trimmed, ":")
			continue
		}
		if pkg == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Candidate:") {
			candidate = strings.TrimSpace(strings.TrimPrefix(trimmed, "Candidate:"))
			continue
		}
		if strings.HasPrefix(trimmed, "Installed:") {
			continue
		}
		if strings.EqualFold(trimmed, "Version table:") {
			inVersionTable = true
			continue
		}
		if !inVersionTable {
			continue
		}
		if strings.HasPrefix(strings.ToLower(trimmed), "release ") {
			// release 行可能出现在 source line 之后（Debian 某些输出），也可能
			// 在之前；两种情况都挂到当前版本的所有 source 上。
			currentRelease = parseAptReleaseLine(strings.TrimSpace(trimmed[len("release "):]))
			for index := range currentSources {
				currentSources[index].Release = currentRelease
			}
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) >= 2 && looksLikeAptSourceLocation(fields[1]) {
			if _, err := strconv.Atoi(fields[0]); err == nil {
				source := parseAptSourceLine(fields)
				source.Release = currentRelease
				currentSources = append(currentSources, source)
				continue
			}
		}
		// 版本条目：可带 `*** ` 标记已安装版本，也可能直接是版本号+优先级。
		version := strings.TrimSpace(strings.TrimPrefix(trimmed, "***"))
		versionFields := strings.Fields(version)
		if len(versionFields) == 0 {
			continue
		}
		startVersion(versionFields[0])
	}
	finishPackage()
	return candidates
}

// looksLikeAptSourceLocation 判断 source line 的第二列是否是来源位置
// （http(s)/ftp/file URL 或本地路径），用来和纯数字版本号区分。
func looksLikeAptSourceLocation(value string) bool {
	return strings.Contains(value, "://") || strings.HasPrefix(value, "/") || strings.HasPrefix(value, "file:")
}

// parseAptSourceLine 解析 source line：`500 http://host/path suite/component arch Packages`。
func parseAptSourceLine(fields []string) aptSource {
	source := aptSource{URL: fields[1]}
	if len(fields) < 3 {
		return source
	}
	suiteComponent := fields[2]
	if suite, component, ok := strings.Cut(suiteComponent, "/"); ok {
		source.Suite = strings.TrimSpace(strings.TrimSuffix(suite, "/"))
		source.Component = strings.TrimSpace(component)
		return source
	}
	// flat 仓库（`./`）没有 component，suite 仍可能包含安全归档名。
	source.Suite = strings.TrimSpace(suiteComponent)
	return source
}

// parseAptReleaseLine 解析 `o=Debian,a=stable-security,n=bookworm-security,...`。
func parseAptReleaseLine(value string) aptRelease {
	var release aptRelease
	for _, field := range strings.Split(value, ",") {
		key, val, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "o", "origin":
			release.Origin = val
		case "a", "archive":
			release.Archive = val
		case "n", "codename":
			release.Codename = val
		case "l", "label":
			release.Label = val
		case "c", "component":
			release.Component = val
		}
	}
	return release
}

// aptOriginKeyPattern 定位 unattended-upgrades 策略块的开头。兼容：
//   - Unattended-Upgrade::Allowed-Origins { ... };
//   - Unattended-Upgrade::Allowed-Origins::<name> { ... };（分块配置）
//   - Unattended-Upgrade::Origins-Pattern { ... };（Debian 2.x 默认写法）
var aptOriginKeyPattern = regexp.MustCompile(`(?is)(?:[a-z0-9_-]+::)?(?:allowed-origins|origins-pattern)\s*(?:::[a-z0-9_-]*)?\s*\{`)

var aptQuotedValuePattern = regexp.MustCompile(`"([^"]*)"`)

// parseAllowedOrigins 从 unattended-upgrades 配置文本解析所有 origin 规则并展开
// ${distro_id}/${distro_codename}。osRelease 缺失时无法展开的规则被丢弃，避免用
// 永远匹配不上的伪规则把候选误判成“非安全”。不包含 security 的 updates 等
// 规则由 securityOriginPatterns 过滤。
func parseAllowedOrigins(text string, osRelease map[string]string) []originPattern {
	cleaned := stripAPTComments(text)
	var patterns []originPattern
	searchFrom := 0
	for searchFrom < len(cleaned) {
		location := aptOriginKeyPattern.FindStringIndex(cleaned[searchFrom:])
		if location == nil {
			break
		}
		content, end := scanAPTBlock(cleaned, searchFrom+location[1])
		if end < 0 {
			break
		}
		searchFrom = end
		for _, value := range aptQuotedValuePattern.FindAllStringSubmatch(content, -1) {
			if len(value) < 2 {
				continue
			}
			pattern, ok := parseOriginPatternValue(value[1], osRelease)
			if ok {
				patterns = append(patterns, pattern)
			}
		}
	}
	return patterns
}

// scanAPTBlock 从 start（位于 `{` 之后）扫描到引号外的第一个 `}`，返回块内容与
// 结束后的位置。变量占位符里的 `}` 位于引号内，不会提前结束块。
func scanAPTBlock(text string, start int) (string, int) {
	inQuote := false
	for index := start; index < len(text); index++ {
		switch text[index] {
		case '"':
			inQuote = !inQuote
		case '}':
			if !inQuote {
				return text[start:index], index + 1
			}
		}
	}
	return "", -1
}

// stripAPTComments 去掉 APT 配置里的 `//` 注释，但保留引号内的内容。
func stripAPTComments(text string) string {
	var b strings.Builder
	inQuote := false
	for index := 0; index < len(text); index++ {
		ch := text[index]
		if ch == '"' {
			inQuote = !inQuote
			b.WriteByte(ch)
			continue
		}
		if !inQuote && ch == '/' && index+1 < len(text) && text[index+1] == '/' {
			for index < len(text) && text[index] != '\n' {
				index++
			}
			if index < len(text) {
				b.WriteByte('\n')
			}
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

// parseOriginPatternValue 解析单条 origin 规则：
//   - "Ubuntu:noble-security"（origin:archive）
//   - "o=Ubuntu,a=noble-security" / "o=Debian,n=bookworm-security,l=Debian-Security"
func parseOriginPatternValue(raw string, osRelease map[string]string) (originPattern, bool) {
	expanded, ok := expandDistroVariables(strings.TrimSpace(raw), osRelease)
	if !ok {
		return originPattern{}, false
	}
	var pattern originPattern
	if strings.Contains(expanded, "=") {
		for _, field := range strings.Split(expanded, ",") {
			key, val, found := strings.Cut(field, "=")
			if !found {
				continue
			}
			val = strings.TrimSpace(val)
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "o", "origin":
				pattern.Origin = val
			case "a", "archive":
				pattern.Archive = val
			case "n", "codename":
				pattern.Codename = val
			case "l", "label":
				pattern.Label = val
			}
		}
	} else if origin, archive, found := strings.Cut(expanded, ":"); found {
		pattern.Origin = strings.TrimSpace(origin)
		pattern.Archive = strings.TrimSpace(archive)
	} else {
		// 单值规则按 archive/suite 名处理。
		pattern.Archive = strings.TrimSpace(expanded)
	}
	if pattern.Archive == "" && pattern.Codename == "" && pattern.Label == "" {
		return originPattern{}, false
	}
	return pattern, true
}

func expandDistroVariables(value string, osRelease map[string]string) (string, bool) {
	codename := strings.TrimSpace(osRelease["UBUNTU_CODENAME"])
	if codename == "" {
		codename = strings.TrimSpace(osRelease["VERSION_CODENAME"])
	}
	distroID := strings.TrimSpace(osRelease["ID"])
	for _, variable := range []string{"${distro_id}", "${distro_release}", "${distro_codename}"} {
		if !strings.Contains(value, variable) {
			continue
		}
		if variable == "${distro_id}" && distroID == "" {
			return "", false
		}
		if variable != "${distro_id}" && codename == "" {
			return "", false
		}
	}
	replacer := strings.NewReplacer(
		"${distro_id}", distroID,
		"${distro_release}", codename,
		"${distro_codename}", codename,
	)
	return replacer.Replace(value), true
}

// securityOriginPatterns 只保留安全归档类的 origin 规则：updates/proposed/
// backports 等允许 origin 即使出现在配置里也不算安全更新。
func securityOriginPatterns(patterns []originPattern) []originPattern {
	security := make([]originPattern, 0, len(patterns))
	for _, pattern := range patterns {
		names := strings.Join([]string{pattern.Archive, pattern.Codename, pattern.Label}, " ")
		if strings.Contains(strings.ToLower(names), "security") {
			security = append(security, pattern)
		}
	}
	return security
}

// loadAllowedOrigins 读取全部 unattended-upgrades 配置文件并返回可用的安全
// origin 规则。文件缺失/无配置不是错误，只是没有额外策略。
func loadAllowedOrigins(paths []string, osRelease map[string]string) []originPattern {
	var patterns []originPattern
	seen := map[string]struct{}{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, pattern := range securityOriginPatterns(parseAllowedOrigins(string(data), osRelease)) {
			key := strings.ToLower(strings.Join([]string{pattern.Origin, pattern.Archive, pattern.Codename, pattern.Label}, "\x00"))
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			patterns = append(patterns, pattern)
		}
	}
	return patterns
}

// defaultAllowedOriginsPaths 返回 /etc/apt/apt.conf.d/ 下 unattended-upgrades
// 相关配置文件（包含 50unattended-upgrades）。glob 失败时退回已知主文件。
func defaultAllowedOriginsPaths() []string {
	matches, err := filepath.Glob(aptSecurityAllowedOriginsGlob)
	if err == nil && len(matches) > 0 {
		sort.Strings(matches)
		return matches
	}
	return []string{"/etc/apt/apt.conf.d/50unattended-upgrades"}
}

// readOSRelease 读取 /etc/os-release 的键值；文件缺失时返回空表，变量展开失败
// 的规则会被 parseAllowedOrigins 丢弃。
func readOSRelease(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	return parseOSRelease(string(data))
}

func parseOSRelease(text string) map[string]string {
	values := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		values[strings.TrimSpace(key)] = val
	}
	return values
}
