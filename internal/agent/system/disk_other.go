//go:build !linux

package system

func readRootDiskUsage() (total, used uint64) {
	return 0, 0
}
