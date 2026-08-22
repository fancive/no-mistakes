//go:build darwin

package legacycleanup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func removeLegacyService(ctx context.Context, path string) error {
	label := strings.TrimSuffix(filepath.Base(path), ".plist")
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), label)
	output, err := exec.CommandContext(ctx, "launchctl", "bootout", target).CombinedOutput()
	if err == nil {
		return nil
	}
	message := strings.ToLower(string(output) + " " + err.Error())
	if strings.Contains(message, "could not find service") || strings.Contains(message, "no such process") || strings.Contains(message, "not found") {
		return nil
	}
	return fmt.Errorf("launchctl bootout %s: %s: %w", target, strings.TrimSpace(string(output)), err)
}
