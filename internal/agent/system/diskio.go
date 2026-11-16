package system

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"
)

// diskSectorSize 是内核块层统计的扇区大小，/proc/diskstats 的 sector 计数
// 单位固定为 512 字节，与设备逻辑扇区大小无关。
const diskSectorSize = 512

// diskIOTotals 是全部整盘块设备的累计读写字节数。速率由报告中心基于相邻
// 读数的时间差计算，与网络速率使用同一累加语义。
type diskIOTotals struct {
	readBytes  int64
	writeBytes int64
}

// readDiskIOTotals 读取 /proc/diskstats，只汇总 /sys/block 下的整盘设备：
// 分区行（如 sda1）与整盘（sda）都会出现在 diskstats 中，同时累加会重复
// 计数；loop/ram/zram 也不承载真实磁盘 IO。任一基础信息读不到时返回错误，
// 调用方必须把磁盘 IO 视为“不可用”，而不是让整个指标停摆。
func readDiskIOTotals() (diskIOTotals, error) {
	whole, err := wholeBlockDevices()
	if err != nil {
		return diskIOTotals{}, err
	}
	file, err := os.Open("/proc/diskstats")
	if err != nil {
		return diskIOTotals{}, err
	}
	defer file.Close()
	return parseDiskStats(file, whole)
}

// wholeBlockDevices 返回 /sys/block 下的整盘设备名集合（如 sda、nvme0n1、
// dm-0、md0）。/sys/block 只列整盘，不列分区。
func wholeBlockDevices() (map[string]struct{}, error) {
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram") {
			continue
		}
		allowed[name] = struct{}{}
	}
	return allowed, nil
}

// parseDiskStats 解析 /proc/diskstats。字段顺序：
// major minor name reads-completed reads-merged sectors-read ms-reading
// writes-completed writes-merged sectors-written ...
// allowed 非 nil 时只汇总其中列出的设备名（整盘）；loop/ram/zram 始终排除。
func parseDiskStats(reader io.Reader, allowed map[string]struct{}) (diskIOTotals, error) {
	var out diskIOTotals
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			continue
		}
		name := fields[2]
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram") {
			continue
		}
		if allowed != nil {
			if _, ok := allowed[name]; !ok {
				continue
			}
		}
		readSectors, err := strconv.ParseInt(fields[5], 10, 64)
		if err != nil {
			continue
		}
		writeSectors, err := strconv.ParseInt(fields[9], 10, 64)
		if err != nil {
			continue
		}
		out.readBytes += readSectors * diskSectorSize
		out.writeBytes += writeSectors * diskSectorSize
	}
	return out, scanner.Err()
}
