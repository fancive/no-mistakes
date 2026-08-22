//go:build windows

package legacycleanup

import "fmt"

func legacySocketActive(path string) (bool, error) {
	return false, fmt.Errorf("cannot prove legacy socket %s inactive on Windows", path)
}
