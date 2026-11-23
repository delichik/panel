// Package sshsessions 读取 Linux 的 utmp/wtmp/btmp，向 Panel 提供 SSH 会话与失败
// 登录记录。解析只依赖标准库，输入路径可注入，便于在非 Linux 环境测试。
//
// 记录格式来自 glibc 的 struct utmp（384 字节定长、小端）：
//
//	0   int16  type
//	4   int32  pid
//	8   char   line[32]
//	44  char   user[32]
//	76  char   host[256]
//	336 int32  session
//	340 int32  tv_sec
//	348 int32  addr_v6[4]
package sshsessions

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	// recordSize 是 struct utmp 的固定长度。
	recordSize = 384

	// 默认路径：Debian/Ubuntu/RHEL 均使用这些位置。
	DefaultUTMPPath = "/var/run/utmp"
	DefaultWTMPPath = "/var/log/wtmp"
	DefaultBTMPPath = "/var/log/btmp"

	// 默认单次最多回报的条目数，避免首次回填把整份 wtmp 推给 Panel。
	DefaultMaxItems = 2000
	// maxSessions 是单次快照的会话上限；正常主机远低于此值。
	maxSessions = 256
	// maxReadBytes 限制单文件读取量，防止超大日志一次占用过多内存。
	maxReadBytes = 64 << 20

	// Linux utmp 类型。
	userProcess = 7
	deadProcess = 8
)

// Options 指定待解析的文件与上限。零值字段使用默认值。
type Options struct {
	UTMPPath        string
	WTMPPath        string
	WTMPRotatedPath string
	BTMPPath        string
	MaxItems        int
}

// Session 是一条当前在线会话（来自 utmp）。
type Session struct {
	UserName      string
	TTY           string
	SourceHost    string
	SourceAddress string
	LoginAt       time.Time
	PID           int
	Kind          string
}

// HistoryItem 是一条历史会话（来自 wtmp，可能尚未配对 logout）。
type HistoryItem struct {
	UserName      string
	TTY           string
	SourceHost    string
	SourceAddress string
	LoginAt       time.Time
	LogoutAt      time.Time
	PID           int
	Kind          string
}

// Failure 是一条失败登录尝试（来自 btmp）。
type Failure struct {
	UserName      string
	SourceHost    string
	SourceAddress string
	AttemptAt     time.Time
	PID           int
}

// History 是 wtmp 的解析结果；Truncated 表示超过 MaxItems 被截断。
type History struct {
	Items     []HistoryItem
	Truncated bool
}

// Failures 是 btmp 的解析结果；Truncated 表示超过 MaxItems 被截断。
type Failures struct {
	Items     []Failure
	Truncated bool
}

// ReadSessions 读取 utmp 中的在线会话。同一 tty 只保留最早登录，避免重复记录
// 与终端复用叠加。utmp 缺失或不可读时返回错误，由调用方决定是否降级。
func ReadSessions(opts Options) ([]Session, error) {
	path := opts.UTMPPath
	if path == "" {
		path = DefaultUTMPPath
	}
	records, err := readRecords(path)
	if err != nil {
		return nil, err
	}
	byTTY := make(map[string]Session)
	for _, rec := range records {
		if rec.typ != userProcess || rec.tty == "" {
			continue
		}
		login := time.Unix(int64(rec.sec), 0)
		if login.Unix() <= 0 {
			continue
		}
		item := Session{
			UserName:      rec.user,
			TTY:           rec.tty,
			SourceHost:    rec.host,
			SourceAddress: rec.address(),
			LoginAt:       login,
			PID:           int(rec.pid),
			Kind:          kindOf(rec.host),
		}
		if existing, ok := byTTY[rec.tty]; !ok || item.LoginAt.Before(existing.LoginAt) {
			byTTY[rec.tty] = item
		}
	}
	items := make([]Session, 0, len(byTTY))
	for _, item := range byTTY {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].LoginAt.Before(items[j].LoginAt) })
	if len(items) > maxSessions {
		items = items[len(items)-maxSessions:]
	}
	return items, nil
}

// ReadHistory 读取 wtmp（含轮转的 .1，先读旧文件）中 since 之后的会话，并按
// (tty, pid) 配对 DEAD_PROCESS 得到登出时间。未配对的会话 LogoutAt 为零值，
// 由 Panel 侧用在线状态兜底。
func ReadHistory(opts Options, since time.Time) (History, error) {
	main := opts.WTMPPath
	if main == "" {
		main = DefaultWTMPPath
	}
	rotated := opts.WTMPRotatedPath
	if rotated == "" {
		rotated = main + ".1"
	}
	paths := []string{main}
	if _, err := os.Stat(rotated); err == nil {
		// 轮转文件更旧，先读；配对顺序因此保持时间顺序。
		paths = []string{rotated, main}
	}
	limit := opts.MaxItems
	if limit <= 0 {
		limit = DefaultMaxItems
	}

	var items []HistoryItem
	open := make(map[string]int)
	seen := make(map[string]struct{})
	for _, path := range paths {
		records, err := readRecords(path)
		if err != nil {
			if path == rotated && !errors.Is(err, os.ErrNotExist) {
				return History{}, err
			}
			if path == main {
				return History{}, err
			}
			continue
		}
		for _, rec := range records {
			at := time.Unix(int64(rec.sec), 0)
			if at.Unix() <= 0 {
				continue
			}
			switch rec.typ {
			case userProcess:
				if rec.tty == "" {
					continue
				}
				if !since.IsZero() && at.Before(since) {
					continue
				}
				sig := fmt.Sprintf("%s|%d|%d", rec.tty, rec.pid, at.Unix())
				if _, ok := seen[sig]; ok {
					continue
				}
				seen[sig] = struct{}{}
				item := HistoryItem{
					UserName:      rec.user,
					TTY:           rec.tty,
					SourceHost:    rec.host,
					SourceAddress: rec.address(),
					LoginAt:       at,
					PID:           int(rec.pid),
					Kind:          kindOf(rec.host),
				}
				items = append(items, item)
				open[fmt.Sprintf("%s|%d", rec.tty, rec.pid)] = len(items) - 1
			case deadProcess:
				key := fmt.Sprintf("%s|%d", rec.tty, rec.pid)
				if idx, ok := open[key]; ok {
					items[idx].LogoutAt = at
					delete(open, key)
				}
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].LoginAt.Before(items[j].LoginAt) })
	truncated := false
	if len(items) > limit {
		items = items[len(items)-limit:]
		truncated = true
	}
	return History{Items: items, Truncated: truncated}, nil
}

// ReadFailures 读取 btmp 中 since 之后的失败登录；每条记录单独保留，不做聚合。
func ReadFailures(opts Options, since time.Time) (Failures, error) {
	path := opts.BTMPPath
	if path == "" {
		path = DefaultBTMPPath
	}
	records, err := readRecords(path)
	if err != nil {
		return Failures{}, err
	}
	limit := opts.MaxItems
	if limit <= 0 {
		limit = DefaultMaxItems
	}
	items := make([]Failure, 0, len(records))
	for _, rec := range records {
		if rec.typ != userProcess {
			continue
		}
		at := time.Unix(int64(rec.sec), 0)
		if at.Unix() <= 0 || (!since.IsZero() && at.Before(since)) {
			continue
		}
		items = append(items, Failure{
			UserName:      rec.user,
			SourceHost:    rec.host,
			SourceAddress: rec.address(),
			AttemptAt:     at,
			PID:           int(rec.pid),
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].AttemptAt.Before(items[j].AttemptAt) })
	truncated := false
	if len(items) > limit {
		items = items[len(items)-limit:]
		truncated = true
	}
	return Failures{Items: items, Truncated: truncated}, nil
}

func kindOf(host string) string {
	if strings.TrimSpace(host) != "" {
		return "ssh"
	}
	return "local"
}

type record struct {
	typ    int16
	pid    int32
	tty    string
	user   string
	host   string
	sec    uint32
	addrV6 [4]uint32
}

// address 把 ut_addr_v6 归一为可读地址：前三个 32 位为零时按 IPv4 处理
// （Linux 把 IPv4 存在最后一个元素），否则按 IPv6。空地址返回空串。
func (r record) address() string {
	a := r.addrV6
	if a[0] == 0 && a[1] == 0 && a[2] == 0 {
		if a[3] == 0 {
			return ""
		}
		return net.IPv4(byte(a[3]), byte(a[3]>>8), byte(a[3]>>16), byte(a[3]>>24)).String()
	}
	buf := make([]byte, 16)
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint32(buf[i*4:], a[i])
	}
	return net.IP(buf).String()
}

func readRecords(path string) ([]record, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	size := info.Size()
	offset := int64(0)
	if size > maxReadBytes {
		offset = size - maxReadBytes
		// 对齐到记录边界，丢弃不完整的首条。
		offset -= offset % recordSize
	}
	if _, err := file.Seek(offset, 0); err != nil {
		return nil, fmt.Errorf("seek %s: %w", path, err)
	}
	data := make([]byte, size-offset)
	if _, err := file.Read(data); err != nil && len(data) > 0 {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	count := len(data) / recordSize
	records := make([]record, 0, count)
	for i := 0; i < count; i++ {
		chunk := data[i*recordSize : (i+1)*recordSize]
		rec := record{
			typ:  int16(binary.LittleEndian.Uint16(chunk[0:])),
			pid:  int32(binary.LittleEndian.Uint32(chunk[4:])),
			tty:  cString(chunk[8:40]),
			user: cString(chunk[44:76]),
			host: cString(chunk[76:332]),
			sec:  binary.LittleEndian.Uint32(chunk[340:]),
		}
		for j := 0; j < 4; j++ {
			rec.addrV6[j] = binary.LittleEndian.Uint32(chunk[348+j*4:])
		}
		records = append(records, rec)
	}
	return records, nil
}

func cString(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(string(b))
}
