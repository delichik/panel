//go:build windows

package docker

import "os"

func fileOwnerMatches(info os.FileInfo, uidValue, gidValue *int) (bool, error) {
	return true, nil
}
