//go:build !linux && !windows

package activitylog

import "errors"

func filesystemCapacity(string) (uint64, uint64, error) {
	return 0, 0, errors.New("capacity inspection unavailable on this platform")
}
