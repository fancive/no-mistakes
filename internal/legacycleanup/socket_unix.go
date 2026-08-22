//go:build !windows

package legacycleanup

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"time"
)

func legacySocketActive(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return false, fmt.Errorf("legacy socket path is not a Unix socket")
	}
	connection, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err == nil {
		_ = connection.Close()
		return true, nil
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENOENT) {
		return false, nil
	}
	return false, err
}
