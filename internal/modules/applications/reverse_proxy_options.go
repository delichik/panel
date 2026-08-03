package applications

import (
	"sort"
	"strconv"
	"strings"

	panelerr "panel/internal/platform/errors"
)

const maxHTTPRouteHeaders = 32

const (
	AnyAccessStrategyRoundRobin    = "round_robin"
	AnyAccessStrategyPrimaryBackup = "primary_backup"
	AnyAccessStrategyIPHash        = "ip_hash"
)

func NormalizeAnyAccessConfig(in AnyAccessConfig, originServerIDs []string) (AnyAccessConfig, error) {
	origins := uniqueSortedStrings(originServerIDs)
	if len(origins) == 0 {
		return AnyAccessConfig{}, panelerr.Validation("reverse_proxy_origin_servers_required", "Reverse proxy route requires at least one origin server")
	}
	strategy := strings.TrimSpace(in.Strategy)
	if strategy == "" {
		strategy = AnyAccessStrategyRoundRobin
	}
	if strategy != AnyAccessStrategyRoundRobin && strategy != AnyAccessStrategyPrimaryBackup && strategy != AnyAccessStrategyIPHash {
		return AnyAccessConfig{}, panelerr.Validation("reverse_proxy_any_access_strategy_invalid", "AnyAccess traffic strategy is invalid")
	}
	primary := strings.TrimSpace(in.PrimaryOriginServerID)
	if in.Enabled && strategy == AnyAccessStrategyPrimaryBackup {
		if primary == "" || !containsStringValue(origins, primary) {
			return AnyAccessConfig{}, panelerr.Validation("reverse_proxy_any_access_primary_origin_invalid", "AnyAccess primary origin server is invalid")
		}
	} else {
		primary = ""
	}
	relay := uniqueSortedStrings(in.RelayServerIDs)
	if !in.Enabled {
		relay = nil
	}
	return AnyAccessConfig{Enabled: in.Enabled, Strategy: strategy, PrimaryOriginServerID: primary, RelayServerIDs: relay}, nil
}

func uniqueSortedStrings(items []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func containsStringValue(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func NormalizeHTTPRouteOptions(in HTTPRouteOptions, proxy, gzip bool, defaultWebSocketMode string) (HTTPRouteOptions, error) {
	out := HTTPRouteOptions{
		GzipMode:              normalizeHTTPRouteMode(in.GzipMode),
		ClientMaxBodySizeMB:   in.ClientMaxBodySizeMB,
		ConnectTimeoutSeconds: in.ConnectTimeoutSeconds,
		ReadTimeoutSeconds:    in.ReadTimeoutSeconds,
		SendTimeoutSeconds:    in.SendTimeoutSeconds,
		BufferingMode:         normalizeHTTPRouteMode(in.BufferingMode),
		WebSocketMode:         strings.TrimSpace(in.WebSocketMode),
	}
	if out.GzipMode == "" {
		return HTTPRouteOptions{}, panelerr.Validation("reverse_proxy_gzip_mode_invalid", "gzip mode is invalid")
	}
	if out.BufferingMode == "" {
		return HTTPRouteOptions{}, panelerr.Validation("reverse_proxy_buffering_mode_invalid", "proxy buffering mode is invalid")
	}
	if !gzip {
		out.GzipMode = HTTPRouteModeInherit
	}
	if !proxy {
		out.ClientMaxBodySizeMB = 0
		out.ConnectTimeoutSeconds = 0
		out.ReadTimeoutSeconds = 0
		out.SendTimeoutSeconds = 0
		out.BufferingMode = HTTPRouteModeInherit
		out.WebSocketMode = HTTPRouteModeOff
		out.RequestHeaders = nil
	} else {
		if out.WebSocketMode == "" {
			out.WebSocketMode = strings.TrimSpace(defaultWebSocketMode)
		}
		if out.WebSocketMode == "" {
			out.WebSocketMode = HTTPRouteModeOff
		}
		if out.WebSocketMode != HTTPRouteWebSocketAuto && out.WebSocketMode != HTTPRouteModeOn && out.WebSocketMode != HTTPRouteModeOff {
			return HTTPRouteOptions{}, panelerr.Validation("reverse_proxy_websocket_mode_invalid", "websocket mode is invalid")
		}
		if err := validateHTTPRouteNumber("client body size", out.ClientMaxBodySizeMB, 10240); err != nil {
			return HTTPRouteOptions{}, err
		}
		if err := validateHTTPRouteNumber("connect timeout", out.ConnectTimeoutSeconds, 300); err != nil {
			return HTTPRouteOptions{}, err
		}
		if err := validateHTTPRouteNumber("read timeout", out.ReadTimeoutSeconds, 3600); err != nil {
			return HTTPRouteOptions{}, err
		}
		if err := validateHTTPRouteNumber("send timeout", out.SendTimeoutSeconds, 3600); err != nil {
			return HTTPRouteOptions{}, err
		}
		var err error
		out.RequestHeaders, err = normalizeHTTPHeaders(in.RequestHeaders, true)
		if err != nil {
			return HTTPRouteOptions{}, err
		}
	}
	var err error
	out.ResponseHeaders, err = normalizeHTTPHeaders(in.ResponseHeaders, false)
	if err != nil {
		return HTTPRouteOptions{}, err
	}
	return out, nil
}

func normalizeHTTPRouteMode(value string) string {
	switch strings.TrimSpace(value) {
	case "", HTTPRouteModeInherit:
		return HTTPRouteModeInherit
	case HTTPRouteModeOn:
		return HTTPRouteModeOn
	case HTTPRouteModeOff:
		return HTTPRouteModeOff
	default:
		return ""
	}
}

func validateHTTPRouteNumber(label string, value, max int) error {
	if value < 0 || value > max {
		return panelerr.Validation("reverse_proxy_option_invalid", label+" is invalid")
	}
	return nil
}

func normalizeHTTPHeaders(items []HTTPHeader, allowEmpty bool) ([]HTTPHeader, error) {
	if len(items) > maxHTTPRouteHeaders {
		return nil, panelerr.Validation("reverse_proxy_headers_too_many", "too many custom headers")
	}
	seen := map[string]struct{}{}
	out := make([]HTTPHeader, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.Name)
		value := item.Value
		if name == "" && value == "" {
			continue
		}
		if len(name) > 128 || !validHTTPHeaderName(name) {
			return nil, panelerr.Validation("reverse_proxy_header_name_invalid", "custom header name is invalid")
		}
		if len(value) > 2048 || strings.ContainsAny(value, "\r\n\x00$") {
			return nil, panelerr.Validation("reverse_proxy_header_value_invalid", "custom header value is invalid")
		}
		if value == "" && !allowEmpty {
			return nil, panelerr.Validation("reverse_proxy_header_value_invalid", "custom response header value is required")
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			return nil, panelerr.Validation("reverse_proxy_header_duplicate", "custom header is duplicated")
		}
		seen[key] = struct{}{}
		out = append(out, HTTPHeader{Name: name, Value: value})
	}
	return out, nil
}

func validHTTPHeaderName(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", r) {
			continue
		}
		return false
	}
	return value != ""
}

func WriteNginxHTTPRouteOptions(b *strings.Builder, options HTTPRouteOptions, indent string, proxy bool) {
	if options.GzipMode == HTTPRouteModeOn {
		b.WriteString(indent + "gzip on;\n")
	} else if options.GzipMode == HTTPRouteModeOff {
		b.WriteString(indent + "gzip off;\n")
	}
	if proxy {
		b.WriteString(indent + "proxy_cache off;\n")
		if options.ClientMaxBodySizeMB > 0 {
			b.WriteString(indent + "client_max_body_size " + strconv.Itoa(options.ClientMaxBodySizeMB) + "m;\n")
		}
		if options.ConnectTimeoutSeconds > 0 {
			b.WriteString(indent + "proxy_connect_timeout " + strconv.Itoa(options.ConnectTimeoutSeconds) + "s;\n")
		}
		if options.ReadTimeoutSeconds > 0 {
			b.WriteString(indent + "proxy_read_timeout " + strconv.Itoa(options.ReadTimeoutSeconds) + "s;\n")
		}
		if options.SendTimeoutSeconds > 0 {
			b.WriteString(indent + "proxy_send_timeout " + strconv.Itoa(options.SendTimeoutSeconds) + "s;\n")
		}
		if options.BufferingMode == HTTPRouteModeOn {
			b.WriteString(indent + "proxy_buffering on;\n")
		} else if options.BufferingMode == HTTPRouteModeOff {
			b.WriteString(indent + "proxy_buffering off;\n")
		}
		for _, header := range options.RequestHeaders {
			b.WriteString(indent + "proxy_set_header " + header.Name + " \"" + escapeNginxQuoted(header.Value) + "\";\n")
		}
	}
	for _, header := range options.ResponseHeaders {
		if proxy {
			b.WriteString(indent + "proxy_hide_header " + header.Name + ";\n")
		}
		b.WriteString(indent + "add_header " + header.Name + " \"" + escapeNginxQuoted(header.Value) + "\" always;\n")
	}
}

func WriteNginxWebSocketOptions(b *strings.Builder, mode, indent string) {
	switch mode {
	case HTTPRouteWebSocketAuto:
		b.WriteString(indent + "proxy_http_version 1.1;\n")
		b.WriteString(indent + "proxy_set_header Upgrade $http_upgrade;\n")
		b.WriteString(indent + "proxy_set_header Connection $connection_upgrade;\n")
	case HTTPRouteModeOn:
		b.WriteString(indent + "proxy_http_version 1.1;\n")
		b.WriteString(indent + "proxy_set_header Upgrade $http_upgrade;\n")
		b.WriteString(indent + "proxy_set_header Connection \"upgrade\";\n")
	}
}

func escapeNginxQuoted(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	return strings.ReplaceAll(value, "\"", "\\\"")
}
