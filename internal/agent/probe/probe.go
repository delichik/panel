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
	// DefaultCount/DefaultTimeoutSeconds 是默认探测口径：3 个包、单包最多
	// 等 2 秒。旧 Panel 不下发 ping 配置时使用。
	DefaultCount          = 3
	DefaultTimeoutSeconds = 2
	// 合法范围：防止异常配置放大探测负载，或让探测协程被长时间占用。
	MinCount          = 1
	MaxCount          = 10
	MinTimeoutSeconds = 1
	MaxTimeoutSeconds = 10
	// minTargetTimeout 是单目标兜底超时的下限。
	minTargetTimeout = 3 * time.Second
)

// Options 是一次探测的 ping 参数。Count 是发包数，TimeoutSeconds 是单包等待
// 秒数（ping -W），均由 Panel 的探测设施配置下发。
type Options struct {
	Count          int
	TimeoutSeconds int
}

// Normalize 把选项归一到合法范围：<=0 回默认值，越界值夹紧到 1..10。
// 返回值而非原地修改，调用方不会因共享 Options 产生串扰。
func (o Options) Normalize() Options {
	if o.Count <= 0 {
		o.Count = DefaultCount
	}
	if o.Count > MaxCount {
		o.Count = MaxCount
	}
	if o.Count < MinCount {
		o.Count = MinCount
	}
	if o.TimeoutSeconds <= 0 {
		o.TimeoutSeconds = DefaultTimeoutSeconds
	}
	if o.TimeoutSeconds > MaxTimeoutSeconds {
		o.TimeoutSeconds = MaxTimeoutSeconds
	}
	if o.TimeoutSeconds < MinTimeoutSeconds {
		o.TimeoutSeconds = MinTimeoutSeconds
	}
	return o
}

// targetTimeout 是单个目标的兜底超时：ping 自身要等 Count*TimeoutSeconds
// 才可能全部超时，这里再加 2 秒留出进程启动和统计行输出时间，且不低于 3 秒。
func (o Options) targetTimeout() time.Duration {
	o = o.Normalize()
	timeout := time.Duration(o.Count*o.TimeoutSeconds)*time.Second + 2*time.Second
	if timeout < minTargetTimeout {
		timeout = minTargetTimeout
	}
	return timeout
}

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

// Run 使用 opts 描述的 ping 参数并发探测全部目标，返回顺序与 targets 一致。
// runner 为 nil 时使用系统 ping；opts 会先归一化，非法值回默认/夹紧。
func Run(ctx context.Context, targets []Target, opts Options, runner Runner) []Result {
	opts = opts.Normalize()
	if runner == nil {
		runner = execRunner(opts)
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
			results[index] = probeTarget(ctx, target, opts, runner)
		}()
	}
	wg.Wait()
	return results
}

func probeTarget(ctx context.Context, target Target, opts Options, runner Runner) Result {
	result := Result{TargetServerID: target.ServerID, TargetAddress: target.Address}
	probeCtx, cancel := context.WithTimeout(ctx, opts.targetTimeout())
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

// execRunner 返回使用系统 ping 的 Runner：-n 不解析域名、-c count 发包数、
// -W timeout 单包等待秒数；IPv6 地址追加 -6。LC_ALL=C 保证统计行可解析。
func execRunner(opts Options) Runner {
	opts = opts.Normalize()
	return func(ctx context.Context, address string) (string, error) {
		cmd := exec.CommandContext(ctx, "ping", pingArgs(address, opts)...)
		cmd.Env = append(os.Environ(), "LC_ALL=C")
		output, err := cmd.CombinedOutput()
		return string(output), err
	}
}

func pingArgs(address string, opts Options) []string {
	opts = opts.Normalize()
	args := make([]string, 0, 8)
	if isIPv6Address(address) {
		args = append(args, "-6")
	}
	args = append(args, "-n", "-c", strconv.Itoa(opts.Count), "-W", strconv.Itoa(opts.TimeoutSeconds), address)
	return args
}

func isIPv6Address(address string) bool {
	trimmed := strings.TrimSpace(strings.Trim(address, "[]"))
	if ip := net.ParseIP(trimmed); ip != nil {
		return ip.To4() == nil
	}
	return strings.Contains(trimmed, ":")
}
