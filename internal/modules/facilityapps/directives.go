package facilityapps

import (
	"strings"

	panelerr "panel/internal/platform/errors"
)

// 自定义 nginx 指令覆盖（FAC-RP-018）的输入约束。上限用于防止管理员输入的
// 配置无限膨胀，同时保证单行指令可以安全地写进 http/server 块顶层。
const (
	customDirectivesMaxLines      = 64
	customDirectiveMaxLineBytes   = 2 * 1024
	customDirectivesMaxTotalBytes = 16 * 1024

	customDirectivesInvalidCode = "facility_custom_directives_invalid"

	directiveScopeHTTP   = "http"
	directiveScopeServer = "server"
)

// nginxDirective 是一个 http/server 块顶层指令（name + 原始参数）。
// location/error_page 等嵌套内容仍由面板生成，自定义指令不进入嵌套块。
type nginxDirective struct {
	Name string
	Args string
}

func (d nginxDirective) line() string {
	if strings.TrimSpace(d.Args) == "" {
		return d.Name + ";"
	}
	return d.Name + " " + strings.TrimSpace(d.Args) + ";"
}

// repeatableDirectives 是 nginx 允许在同一作用域重复出现、且语义为"多条叠加"
// 的指令。其余同名指令只保留最后一条，避免 nginx "duplicate directive" 报错。
var repeatableDirectives = map[string]struct{}{
	"add_header":        {},
	"proxy_set_header":  {},
	"proxy_hide_header": {},
}

// forbiddenDirectives 是面板保留的安全/身份项：这些指令决定监听端口、域名、
// 证书、模块、上游结构与嵌套块，允许自定义会直接破坏入口代理或与面板生成项
// 冲突。resolver 只在 http 作用域禁用（http 级只允许面板单例）。
var forbiddenDirectives = map[string]struct{}{
	"listen":               {},
	"server_name":          {},
	"ssl_certificate":      {},
	"ssl_certificate_key":  {},
	"ssl_certificate_path": {},
	"include":              {},
	"load_module":          {},
	"root":                 {},
	"alias":                {},
	"proxy_pass":           {},
	"upstream":             {},
	"map":                  {},
	"if":                   {},
	"location":             {},
	"limit_except":         {},
}

var forbiddenHTTPDirectives = map[string]struct{}{
	"resolver": {},
}

// normalizeCustomDirectiveLines 校验并归一化自定义指令行，返回可直接持久化与
// 渲染的规范行（每行以 ';' 结尾、同名非重复指令保留最后一条）。
func normalizeCustomDirectiveLines(scope string, lines []string) ([]string, []nginxDirective, error) {
	directives, err := parseCustomDirectives(scope, lines)
	if err != nil {
		return nil, nil, err
	}
	out := make([]string, 0, len(directives))
	for _, directive := range directives {
		out = append(out, directive.line())
	}
	return out, directives, nil
}

// parseCustomDirectives 把用户输入解析成结构化的 http/server 顶层指令列表。
// 只接受单行无块指令；带 '{' / '}' / '#' / NUL / 换行的输入一律拒绝。
func parseCustomDirectives(scope string, lines []string) ([]nginxDirective, error) {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = directiveScopeServer
	}
	stripped := make([]string, 0, len(lines))
	totalBytes := 0
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		stripped = append(stripped, line)
		totalBytes += len(line)
	}
	if len(stripped) > customDirectivesMaxLines {
		return nil, customDirectiveError(scope, 0, "", "too_many_lines")
	}
	if totalBytes > customDirectivesMaxTotalBytes {
		return nil, customDirectiveError(scope, 0, "", "too_large")
	}
	out := make([]nginxDirective, 0, len(stripped))
	indexByName := map[string]int{}
	for index, line := range stripped {
		lineNo := index + 1
		if len(line) > customDirectiveMaxLineBytes {
			return nil, customDirectiveError(scope, lineNo, "", "line_too_long")
		}
		if strings.ContainsAny(line, "{}\x00\r\n#") {
			return nil, customDirectiveError(scope, lineNo, "", "invalid_characters")
		}
		if strings.HasSuffix(line, ";") {
			line = strings.TrimSpace(strings.TrimSuffix(line, ";"))
		}
		if line == "" || strings.Contains(line, ";") {
			return nil, customDirectiveError(scope, lineNo, "", "invalid_syntax")
		}
		name, args := splitNginxDirective(line)
		if !validNginxDirectiveName(name) {
			return nil, customDirectiveError(scope, lineNo, name, "invalid_name")
		}
		if _, forbidden := forbiddenDirectives[name]; forbidden {
			return nil, customDirectiveError(scope, lineNo, name, "forbidden_directive")
		}
		if scope == directiveScopeHTTP {
			if _, forbidden := forbiddenHTTPDirectives[name]; forbidden {
				return nil, customDirectiveError(scope, lineNo, name, "forbidden_directive")
			}
		}
		directive := nginxDirective{Name: name, Args: args}
		if _, repeatable := repeatableDirectives[name]; !repeatable {
			if existing, ok := indexByName[name]; ok {
				out[existing] = directive
				continue
			}
		}
		indexByName[name] = len(out)
		out = append(out, directive)
	}
	return out, nil
}

func splitNginxDirective(line string) (string, string) {
	if index := strings.IndexAny(line, " \t"); index >= 0 {
		return line[:index], strings.TrimSpace(line[index:])
	}
	return line, ""
}

func validNginxDirectiveName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for index, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9' && index > 0:
		case r == '_' && index > 0:
		default:
			return false
		}
	}
	return true
}

func customDirectiveError(scope string, line int, name, reason string) error {
	details := map[string]any{"scope": scope, "reason": reason}
	if line > 0 {
		details["line"] = line
	}
	if strings.TrimSpace(name) != "" {
		details["directive"] = name
	}
	return panelerr.WithDetails(panelerr.Validation(customDirectivesInvalidCode, "custom nginx directive is invalid"), details)
}

// canonicalCustomDirectiveLines 在渲染时重新解析一次持久化的指令行。
// 已持久化的数据理论上一定合法；解析失败时退化忽略，避免渲染直接失败。
func canonicalCustomDirectiveLines(lines []string) []nginxDirective {
	directives, err := parseCustomDirectives(directiveScopeServer, lines)
	if err != nil {
		return nil
	}
	return directives
}

// mergeNginxDirectives 实现"同名覆盖、其余追加"：自定义指令与面板生成的顶层
// 指令同名时丢弃面板生成项，并在原位置输出全部自定义项；不同名自定义项追加
// 到列表末尾。
func mergeNginxDirectives(base, custom []nginxDirective) []nginxDirective {
	if len(custom) == 0 {
		return base
	}
	customByName := map[string][]nginxDirective{}
	baseNames := map[string]struct{}{}
	for _, directive := range base {
		baseNames[directive.Name] = struct{}{}
	}
	for _, directive := range custom {
		customByName[directive.Name] = append(customByName[directive.Name], directive)
	}
	out := make([]nginxDirective, 0, len(base)+len(custom))
	written := map[string]struct{}{}
	for _, directive := range base {
		replacement, overridden := customByName[directive.Name]
		if !overridden {
			out = append(out, directive)
			continue
		}
		if _, done := written[directive.Name]; done {
			continue
		}
		written[directive.Name] = struct{}{}
		out = append(out, replacement...)
	}
	for _, directive := range custom {
		if _, ok := baseNames[directive.Name]; ok {
			continue
		}
		if _, done := written[directive.Name]; done {
			continue
		}
		written[directive.Name] = struct{}{}
		out = append(out, customByName[directive.Name]...)
	}
	return out
}

// writeNginxDirectives 把结构化顶层指令写入块内容（调用方负责缩进上下文）。
func writeNginxDirectives(b *strings.Builder, indent string, directives []nginxDirective) {
	for _, directive := range directives {
		b.WriteString(indent + directive.line() + "\n")
	}
}
