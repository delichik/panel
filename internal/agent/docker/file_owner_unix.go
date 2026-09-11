//go:build !windows

package docker

import (
	"os"
	"syscall"
)

func fileOwnerMatches(info os.FileInfo, uidValue, gidValue *int) (bool, error) {
	if uidValue == nil && gidValue == nil {
		return true, nil
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return true, nil
	}
	if uidValue != nil && int(stat.Uid) != *uidValue {
		return false, nil
	}
	if gidValue != nil && int(stat.Gid) != *gidValue {
		return false, nil
	}
	return true, nil
}
