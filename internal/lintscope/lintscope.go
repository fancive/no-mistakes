// Package lintscope runs an explicitly authorized repository lint command
// against a deterministic, NUL-delimited exact-file manifest.
package lintscope

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/kunchenguid/no-mistakes/internal/git"
	"github.com/kunchenguid/no-mistakes/internal/shellenv"
)

const (
	BaseSHAEnv          = "NO_MISTAKES_BASE_SHA"
	HeadSHAEnv          = "NO_MISTAKES_HEAD_SHA"
	ChangedFilesFileEnv = "NO_MISTAKES_CHANGED_FILES_FILE"
	ScopeEnv            = "NO_MISTAKES_LINT_SCOPE"
)

// Options is the complete synchronous lint contract.
type Options struct {
	Dir     string
	Command string
	Files   []string
	BaseSHA string
	HeadSHA string
	Env     []string
}

// Prepared is the immutable command environment produced by Prepare.
type Prepared struct {
	Command      string
	Files        []string
	ManifestPath string
	Env          []string
}

// Result records deterministic lint execution evidence.
type Result struct {
	Command  string
	Files    []string
	Output   string
	ExitCode int
}

// Runner executes a configured lint command without discovering or fixing.
type Runner struct{}

// Prepare validates Files, keeps tracked deletions, and writes their sorted
// repository-relative names to a temporary NUL manifest.
func Prepare(options Options) (Prepared, func(), error) {
	root, err := filepath.Abs(options.Dir)
	if err != nil {
		return Prepared{}, func() {}, fmt.Errorf("resolve lint repository: %w", err)
	}
	seen := make(map[string]struct{}, len(options.Files))
	files := make([]string, 0, len(options.Files))
	for _, value := range options.Files {
		if value == "" || filepath.IsAbs(value) {
			return Prepared{}, func() {}, fmt.Errorf("lint file must name a file inside the repository: %q", value)
		}
		clean := filepath.Clean(value)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return Prepared{}, func() {}, fmt.Errorf("lint file must name a file inside the repository: %q", value)
		}
		path := filepath.ToSlash(clean)
		if _, ok := seen[path]; ok {
			continue
		}
		full := filepath.Join(root, clean)
		info, statErr := os.Lstat(full)
		switch {
		case os.IsNotExist(statErr):
			if _, trackedErr := git.Run(context.Background(), root, "--literal-pathspecs", "ls-files", "--error-unmatch", "--", path); trackedErr != nil {
				trackedAtHead, headErr := git.RunRaw(context.Background(), root, "--literal-pathspecs", "ls-tree", "--name-only", "-z", "HEAD", "--", path)
				want := append([]byte(path), 0)
				if headErr != nil || !bytes.Equal(trackedAtHead, want) {
					return Prepared{}, func() {}, fmt.Errorf("lint file does not exist and is not a tracked deletion: %q", path)
				}
			}
		case statErr != nil:
			return Prepared{}, func() {}, fmt.Errorf("inspect lint file %q: %w", path, statErr)
		case info.IsDir():
			return Prepared{}, func() {}, fmt.Errorf("lint file must be an individual file: %q", path)
		}
		seen[path] = struct{}{}
		files = append(files, path)
	}
	sort.Strings(files)

	manifest, err := os.CreateTemp("", "no-mistakes-lint-*.nul")
	if err != nil {
		return Prepared{}, func() {}, fmt.Errorf("create lint manifest: %w", err)
	}
	manifestPath, err := filepath.Abs(manifest.Name())
	if err != nil {
		_ = manifest.Close()
		_ = os.Remove(manifest.Name())
		return Prepared{}, func() {}, fmt.Errorf("resolve lint manifest: %w", err)
	}
	cleanup := func() { _ = os.Remove(manifestPath) }
	if len(files) > 0 {
		if _, err := manifest.WriteString(strings.Join(files, "\x00") + "\x00"); err != nil {
			_ = manifest.Close()
			cleanup()
			return Prepared{}, func() {}, fmt.Errorf("write lint manifest: %w", err)
		}
	}
	if err := manifest.Close(); err != nil {
		cleanup()
		return Prepared{}, func() {}, fmt.Errorf("close lint manifest: %w", err)
	}

	env := options.Env
	if env == nil {
		env = os.Environ()
	}
	env = envWithOverrides(env,
		BaseSHAEnv+"="+options.BaseSHA,
		HeadSHAEnv+"="+options.HeadSHA,
		ChangedFilesFileEnv+"="+manifestPath,
		ScopeEnv+"=changed",
	)
	return Prepared{Command: options.Command, Files: files, ManifestPath: manifestPath, Env: env}, cleanup, nil
}

// Run executes the configured command through the platform shell.
func (Runner) Run(ctx context.Context, options Options) (Result, error) {
	if strings.TrimSpace(options.Command) == "" {
		return Result{}, fmt.Errorf("repository lint command is not configured")
	}
	prepared, cleanup, err := Prepare(options)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/d", "/s", "/c", prepared.Command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", prepared.Command)
	}
	cmd.Dir = options.Dir
	cmd.Env = prepared.Env
	shellenv.ConfigureShellCommand(cmd)
	output, runErr := shellenv.CombinedOutputShellCommand(cmd)
	result := Result{Command: prepared.Command, Files: prepared.Files, Output: string(output)}
	if runErr == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		return result, fmt.Errorf("run repository lint: %w", runErr)
	}
	result.ExitCode = exitErr.ExitCode()
	return result, fmt.Errorf("repository lint failed with exit code %d: %s", result.ExitCode, strings.TrimSpace(result.Output))
}

func envWithOverrides(env []string, overrides ...string) []string {
	keys := make(map[string]struct{}, len(overrides))
	for _, entry := range overrides {
		keys[envKey(entry)] = struct{}{}
	}
	out := make([]string, 0, len(env)+len(overrides))
	for _, entry := range env {
		if _, replaced := keys[envKey(entry)]; !replaced {
			out = append(out, entry)
		}
	}
	return append(out, overrides...)
}

func envKey(entry string) string {
	key, _, _ := strings.Cut(entry, "=")
	return key
}
