// Package probe 在探测节点上执行系统 ping，测量到目标节点的延迟与丢包。
// 该包不依赖任何特权接口或第三方网络库：只用节点自带的 ping 命令，结果逐目标
// 返回，单个目标失败不影响同一轮的其他目标，也不会影响指标上报。
package probe

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// MaxConcurrency 是单轮探测的最大并发数，避免一次探测高峰影响节点网络。
	MaxConcurrency = 8
	// perTargetTimeout 是单个目标的探测预算。ping 自身用 -c 3 -W 2 控制发包，
	// 最坏情况（3 个包全部超时）约 6 秒，因此预算留到 7 秒，保证仍能拿到
	// 统计行；这里再兜底一次，防止 ping 进程卡死占用探测协程。
	perTargetTimeout = 7 * time.Second
	// pingCount/pingWaitSeconds 与 Panel 的探测口径保持一致。
	pingCount       = 3
	pingWaitSeconds = 2
)

// Target 是一个探测目标：Panel 已排除探测节点自身，地址取自目标服务器的
// IPv4、IPv6 或主机名。
type Target struct {
	ServerID string
	Address  string
	Name     string
}

// Result 是对单个目标的一次测量结果。Error 非空表示本次探测未能得到有效
// 延迟/丢包（例如 ping 不可用、输出无法解析或全部超时）。
type Result struct {
	TargetServerID    string
	TargetAddress     string
	LatencyMS         float64
	PacketLossPercent float64
	Sent              int
	Received          int
	Error             string
}

// Stats 是一次 ping 输出解析出的统计值。
type Stats struct {
	Sent              int
	Received          int
	PacketLossPercent float64
	LatencyMS         float64
}

// Runner 执行一次 ping 命令并返回合并后的输出。传入的 ctx 已带单目标超时。
// 测试可注入替身，生产使用系统 ping。
type Runner func(ctx context.Context, address string) (string, error)

var (
	// 兼容 iputils（"3 packets transmitted, 3 received, 0% packet loss"）
	// 与 busybox（"3 packets transmitted, 3 packets received, 0% packet loss"）。
	packetsPattern  = regexp.MustCompile(`(\d+)\s+packets transmitted,\s+(\d+)(?:\s+packets)?\s+received`)
	lossPattern     = regexp.MustCompile(`([0-9.]+)%\s+packet loss`)
	rttPattern      = regexp.MustCompile(`(?:rtt|round-trip)\s+min/avg/max(?:/mdev)?\s*=\s*[0-9.]+/([0-9.]+)/[0-9.]+`)
	icmpTimePattern = regexp.MustCompile(`time[=<]([0-9.]+)\s*ms`)
)

// Run 并发探测全部目标，返回顺序与 targets 一致。runner 为 nil 时使用系统 ping。
func Run(ctx context.Context, targets []Target, runner Runner) []Result {
	if runner == nil {
		runner = execRunner
	}
	results := make([]Result, len(targets))
	sem := make(chan struct{}, MaxConcurrency)
	var wg sync.WaitGroup
	for index, target := range targets {
		index, target := index, target
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[index] = Result{TargetServerID: target.ServerID, TargetAddress: target.Address, Error: ctx.Err().Error()}
				return
			}
			results[index] = probeTarget(ctx, target, runner)
		}()
	}
	wg.Wait()
	return results
}

func probeTarget(ctx context.Context, target Target, runner Runner) Result {
	result := Result{TargetServerID: target.ServerID, TargetAddress: target.Address}
	probeCtx, cancel := context.WithTimeout(ctx, perTargetTimeout)
	defer cancel()
	output, runErr := runner(probeCtx, target.Address)
	stats, parseErr := ParsePingOutput(output)
	if parseErr != nil {
		result.Error = describeFailure(runErr, parseErr)
		return result
	}
	result.Sent = stats.Sent
	result.Received = stats.Received
	result.PacketLossPercent = stats.PacketLossPercent
	result.LatencyMS = stats.LatencyMS
	if stats.Received == 0 {
		// 全部丢包时延迟没有意义，但丢包率仍是有效观测；Error 说明没有收到回复。
		result.Error = describeFailure(runErr, errors.New("no reply received"))
	}
	return result
}

func describeFailure(runErr, fallback error) string {
	if runErr != nil {
		return runErr.Error()
	}
	return fallback.Error()
}

// ParsePingOutput 解析 iputils/busybox 的 ping 统计输出：发包/收包数、丢包率，
// 以及 rtt min/avg/max 的平均值。缺少统计行时返回错误；缺少 rtt 行但能解析
// 单个回复的 time= 时，用回复时间的平均值兜底。
func ParsePingOutput(output string) (Stats, error) {
	match := packetsPattern.FindStringSubmatch(output)
	if match == nil {
		return Stats{}, errors.New("ping output has no packet statistics")
	}
	var stats Stats
	stats.Sent, _ = strconv.Atoi(match[1])
	stats.Received, _ = strconv.Atoi(match[2])
	if loss := lossPattern.FindStringSubmatch(output); loss != nil {
		stats.PacketLossPercent, _ = strconv.ParseFloat(loss[1], 64)
	} else if stats.Sent > 0 {
		stats.PacketLossPercent = 100 * float64(stats.Sent-stats.Received) / float64(stats.Sent)
	}
	if rtt := rttPattern.FindStringSubmatch(output); rtt != nil {
		stats.LatencyMS, _ = strconv.ParseFloat(rtt[1], 64)
		return stats, nil
	}
	var sum float64
	var count int
	for _, value := range icmpTimePattern.FindAllStringSubmatch(output, -1) {
		parsed, err := strconv.ParseFloat(value[1], 64)
		if err != nil {
			continue
		}
		sum += parsed
		count++
	}
	if count > 0 {
		stats.LatencyMS = sum / float64(count)
	}
	return stats, nil
}

// execRunner 用系统 ping 测量目标：-n 不解析域名、-c 3 发 3 个包、
// -W 2 每个包最多等 2 秒；IPv6 地址追加 -6。LC_ALL=C 保证统计行可解析。
func execRunner(ctx context.Context, address string) (string, error) {
	cmd := exec.CommandContext(ctx, "ping", pingArgs(address)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func pingArgs(address string) []string {
	args := make([]string, 0, 8)
	if isIPv6Address(address) {
		args = append(args, "-6")
	}
	args = append(args, "-n", "-c", strconv.Itoa(pingCount), "-W", strconv.Itoa(pingWaitSeconds), address)
	return args
}

func isIPv6Address(address string) bool {
	trimmed := strings.TrimSpace(strings.Trim(address, "[]"))
	if ip := net.ParseIP(trimmed); ip != nil {
		return ip.To4() == nil
	}
	return strings.Contains(trimmed, ":")
}
