//go:build linux

package legacycleanup

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func removeLegacyService(ctx context.Context, path string) error {
	unit := filepath.Base(path)
	output, err := exec.CommandContext(ctx, "systemctl", "--user", "disable", "--now", unit).CombinedOutput()
	if err != nil {
		message := strings.ToLower(string(output) + " " + err.Error())
		if !strings.Contains(message, "not loaded") && !strings.Contains(message, "does not exist") && !strings.Contains(message, "not found") {
			return fmt.Errorf("systemctl --user disable --now %s: %s: %w", unit, strings.TrimSpace(string(output)), err)
		}
	}
	if output, err = exec.CommandContext(ctx, "systemctl", "--user", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}
