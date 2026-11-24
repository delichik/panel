// Package sshsessions 读取 Linux 的 utmp/wtmp/btmp 以及 sshd 认证日志（systemd
// journal / auth.log），向 Panel 提供 SSH 会话与失败登录记录。解析只依赖标准库，
// 输入路径与日志读取器可注入，便于在非 Linux 环境测试。
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
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
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
	DefaultUTMPPath    = "/var/run/utmp"
	DefaultWTMPPath    = "/var/log/wtmp"
	DefaultBTMPPath    = "/var/log/btmp"
	DefaultAuthLogPath = "/var/log/auth.log"

	// 默认单次最多回报的条目数，避免首次回填把整份日志推给 Panel。
	DefaultMaxItems = 2000
	// maxSessions 是单次快照的会话上限；正常主机远低于此值。
	maxSessions = 256
	// maxReadBytes 限制单文件读取量，防止超大日志一次占用过多内存。
	maxReadBytes = 64 << 20

	// Linux utmp 类型（<utmp.h>）。
	bootTime     = 2
	loginProcess = 6
	userProcess  = 7
	deadProcess  = 8
)

// Options 指定待解析的文件、日志源与上限。零值字段使用默认值。
type Options struct {
	UTMPPath        string
	WTMPPath        string
	WTMPRotatedPath string
	BTMPPath        string
	BTMPRotatedPath string
	AuthLogPath     string
	// DisableJournal 关闭 systemd journalctl 查询，用于仅测试文件解析路径。
	DisableJournal bool
	// JournalReader 可注入自定义 journal 读取器，便于测试。
	JournalReader func(ctx context.Context, since time.Time) ([]Failure, error)
	MaxItems      int
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

// Failure 是一条失败登录尝试（来自 btmp 或 sshd 日志）。
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

// Failures 是失败登录的解析结果；Truncated 表示超过 MaxItems 被截断。
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
		addr := resolveAddress(rec.host, rec.address())
		item := Session{
			UserName:      rec.user,
			TTY:           rec.tty,
			SourceHost:    rec.host,
			SourceAddress: addr,
			LoginAt:       login,
			PID:           int(rec.pid),
			Kind:          kindOf(rec.host, addr),
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
// (tty, pid) 优先、tty 兜底配对 DEAD_PROCESS 得到登出时间，同时在 BOOT_TIME
// 或同 tty 复用时关闭旧会话。未配对的会话 LogoutAt 为零值，由 Panel 侧用在线状态兜底。
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

	var allItems []HistoryItem
	openByKey := make(map[string]int)
	openByTTY := make(map[string]int)
	seen := make(map[string]struct{})
	closeOpen := func(idx int, at time.Time) {
		if idx < 0 || idx >= len(allItems) {
			return
		}
		if !allItems[idx].LogoutAt.IsZero() || at.Before(allItems[idx].LoginAt) {
			return
		}
		allItems[idx].LogoutAt = at
		key := fmt.Sprintf("%s|%d", allItems[idx].TTY, allItems[idx].PID)
		if cur, ok := openByKey[key]; ok && cur == idx {
			delete(openByKey, key)
		}
		if cur, ok := openByTTY[allItems[idx].TTY]; ok && cur == idx {
			delete(openByTTY, allItems[idx].TTY)
		}
	}

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
			case bootTime:
				// 系统重启会终结此前所有未关闭的会话。
				for _, idx := range openByTTY {
					closeOpen(idx, at)
				}
				for _, idx := range openByKey {
					closeOpen(idx, at)
				}
			case userProcess:
				if rec.tty == "" {
					continue
				}
				sig := fmt.Sprintf("%s|%d|%d", rec.tty, rec.pid, at.Unix())
				if _, ok := seen[sig]; ok {
					continue
				}
				seen[sig] = struct{}{}
				if prevIdx, ok := openByTTY[rec.tty]; ok && !at.Before(allItems[prevIdx].LoginAt) {
					closeOpen(prevIdx, at)
				}
				addr := resolveAddress(rec.host, rec.address())
				item := HistoryItem{
					UserName:      rec.user,
					TTY:           rec.tty,
					SourceHost:    rec.host,
					SourceAddress: addr,
					LoginAt:       at,
					PID:           int(rec.pid),
					Kind:          kindOf(rec.host, addr),
				}
				allItems = append(allItems, item)
				idx := len(allItems) - 1
				openByKey[fmt.Sprintf("%s|%d", rec.tty, rec.pid)] = idx
				openByTTY[rec.tty] = idx
			case deadProcess:
				if rec.tty == "" {
					continue
				}
				key := fmt.Sprintf("%s|%d", rec.tty, rec.pid)
				if idx, ok := openByKey[key]; ok {
					closeOpen(idx, at)
				} else if idx, ok := openByTTY[rec.tty]; ok {
					// Linux wtmp 中 DEAD_PROCESS 的 ut_pid 有时为 0 或与登录进程不同，
					// 同一 tty 同时只能有一个活跃会话，按 tty 兜底配对。
					closeOpen(idx, at)
				}
			}
		}
	}

	items := make([]HistoryItem, 0, len(allItems))
	for _, item := range allItems {
		if !since.IsZero() && item.LoginAt.Before(since) && (item.LogoutAt.IsZero() || item.LogoutAt.Before(since)) {
			continue
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].LoginAt.Before(items[j].LoginAt) })
	truncated := false
	if len(items) > limit {
		items = items[len(items)-limit:]
		truncated = true
	}
	return History{Items: items, Truncated: truncated}, nil
}

// ReadFailures 读取 btmp（含轮转 .1）中 since 之后的失败登录；每条记录单独保留，
// 不做聚合。Linux PAM/sshd 写入 btmp 时常使用 LOGIN_PROCESS(6) 或 USER_PROCESS(7)，
// 两者均须识别。
func ReadFailures(opts Options, since time.Time) (Failures, error) {
	main := opts.BTMPPath
	if main == "" {
		main = DefaultBTMPPath
	}
	rotated := opts.BTMPRotatedPath
	if rotated == "" {
		rotated = main + ".1"
	}
	paths := []string{main}
	if _, err := os.Stat(rotated); err == nil {
		paths = []string{rotated, main}
	}
	limit := opts.MaxItems
	if limit <= 0 {
		limit = DefaultMaxItems
	}
	var items []Failure
	for _, path := range paths {
		records, err := readRecords(path)
		if err != nil {
			if path == rotated && !errors.Is(err, os.ErrNotExist) {
				return Failures{}, err
			}
			if path == main {
				return Failures{}, err
			}
			continue
		}
		for _, rec := range records {
			if rec.typ != userProcess && rec.typ != loginProcess {
				continue
			}
			at := time.Unix(int64(rec.sec), 0)
			if at.Unix() <= 0 || (!since.IsZero() && at.Before(since)) {
				continue
			}
			addr := resolveAddress(rec.host, rec.address())
			if rec.user == "" && rec.host == "" && addr == "" {
				continue
			}
			items = append(items, Failure{
				UserName:      rec.user,
				SourceHost:    rec.host,
				SourceAddress: addr,
				AttemptAt:     at,
				PID:           int(rec.pid),
			})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].AttemptAt.Before(items[j].AttemptAt) })
	truncated := false
	if len(items) > limit {
		items = items[len(items)-limit:]
		truncated = true
	}
	return Failures{Items: items, Truncated: truncated}, nil
}

func kindOf(host, addr string) string {
	if strings.TrimSpace(host) != "" || strings.TrimSpace(addr) != "" {
		return "ssh"
	}
	return "local"
}

func resolveAddress(host, addr string) string {
	if addr != "" {
		return addr
	}
	if ip := net.ParseIP(strings.TrimSpace(host)); ip != nil {
		return ip.String()
	}
	return ""
}

type record struct {
	typ  int16
	pid  int32
	tty  string
	user string
	host string
	sec  uint32
	addr [16]byte
}

// address 把 ut_addr_v6 归一为可读地址：glibc 将 IPv4 存在 ut_addr_v6[0]
// （前 4 字节，网络字节序，后 12 字节全零），IPv6 占完整 16 字节。全零返回空串。
func (r record) address() string {
	allZeroTail := true
	for i := 4; i < 16; i++ {
		if r.addr[i] != 0 {
			allZeroTail = false
			break
		}
	}
	if allZeroTail {
		if r.addr[0] == 0 && r.addr[1] == 0 && r.addr[2] == 0 && r.addr[3] == 0 {
			return ""
		}
		return net.IPv4(r.addr[0], r.addr[1], r.addr[2], r.addr[3]).String()
	}
	return net.IP(r.addr[:]).String()
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
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek %s: %w", path, err)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxReadBytes))
	if err != nil {
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
		copy(rec.addr[:], chunk[348:364])
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
