//go:build windows

package activitylog

import "golang.org/x/sys/windows"

func filesystemCapacity(path string) (uint64, uint64, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var available, total, free uint64
	err = windows.GetDiskFreeSpaceEx(p, &available, &total, &free)
	return available, total, err
}
