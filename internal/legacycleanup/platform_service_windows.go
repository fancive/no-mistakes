//go:build windows

package legacycleanup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

func discoverLegacyPlatformServices(ctx context.Context, root string) ([]Target, []string) {
	names := []string{"no-mistakes-daemon-" + hashString(strings.ToLower(root))[:8], "no-mistakes-daemon"}
	var targets []Target
	var blockers []string
	for _, name := range names {
		output, err := exec.CommandContext(ctx, "schtasks", "/Query", "/TN", name, "/XML").CombinedOutput()
		if err != nil {
			message := strings.ToLower(string(output) + " " + err.Error())
			if strings.Contains(message, "cannot find") || strings.Contains(message, "not found") {
				continue
			}
			blockers = append(blockers, "Windows scheduled-task state is uncertain for "+name+": "+strings.TrimSpace(string(output)))
			continue
		}
		text := string(output)
		lower := strings.ToLower(text)
		if !strings.Contains(lower, strings.ToLower(root)) || !strings.Contains(lower, "no-mistakes") || !strings.Contains(lower, "daemon") {
			blockers = append(blockers, "scheduled-task ownership is uncertain: "+name)
			continue
		}
		state, stateErr := windowsScheduledTaskState(ctx, name)
		if stateErr != nil {
			blockers = append(blockers, "scheduled-task runtime state is uncertain: "+name+": "+stateErr.Error())
			continue
		}
		if state == 4 {
			blockers = append(blockers, "legacy scheduled task is still running: "+name)
			continue
		}
		targets = append(targets, Target{
			Kind: "scheduled-task", Display: name, ExpectedURL: root,
			Fingerprint: hashString(name + "\x00" + root + "\x00" + text),
		})
	}
	return targets, blockers
}

func removeLegacyPlatformService(ctx context.Context, target Target) error {
	output, err := exec.CommandContext(ctx, "schtasks", "/Query", "/TN", target.Display, "/XML").CombinedOutput()
	if err != nil {
		return fmt.Errorf("recheck scheduled task: %s: %w", strings.TrimSpace(string(output)), err)
	}
	if hashString(target.Display+"\x00"+target.ExpectedURL+"\x00"+string(output)) != target.Fingerprint {
		return fmt.Errorf("scheduled-task fingerprint changed")
	}
	state, err := windowsScheduledTaskState(ctx, target.Display)
	if err != nil {
		return fmt.Errorf("recheck scheduled-task runtime state: %w", err)
	}
	if state == 4 {
		return fmt.Errorf("scheduled task became active before cleanup")
	}
	if output, err = exec.CommandContext(ctx, "schtasks", "/Delete", "/TN", target.Display, "/F").CombinedOutput(); err != nil {
		return fmt.Errorf("delete scheduled task: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func windowsScheduledTaskState(ctx context.Context, name string) (int, error) {
	escaped := strings.ReplaceAll(name, "'", "''")
	command := "$task=Get-ScheduledTask -TaskName '" + escaped + "';Write-Output ([int]$task.State)"
	output, err := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("query task state: %s: %w", strings.TrimSpace(string(output)), err)
	}
	value := strings.TrimSpace(string(output))
	switch value {
	case "1":
		return 1, nil
	case "2":
		return 2, nil
	case "3":
		return 3, nil
	case "4":
		return 4, nil
	default:
		return 0, fmt.Errorf("unexpected task state %q", value)
	}
}
