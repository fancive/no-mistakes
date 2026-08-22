// Package git contains the small Git boundary used by the stateless guard.
package git

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kunchenguid/no-mistakes/internal/safeurl"
	"github.com/kunchenguid/no-mistakes/internal/winproc"
)

// Run executes Git in dir and returns trimmed stdout.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := RunRaw(ctx, dir, args...)
	return strings.TrimSpace(string(out)), err
}

// RunRaw executes Git in dir and returns stdout without byte normalization.
func RunRaw(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return runRawWithEnv(ctx, dir, nil, args...)
}

func runRawWithEnv(ctx context.Context, dir string, extraEnv []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(NonInteractiveEnv(dir), extraEnv...)
	winproc.Harden(cmd)
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("git %s: %w: %s", safeurl.RedactText(strings.Join(args, " ")), err, safeurl.RedactText(stderr))
	}
	return out, nil
}

// FindGitRoot resolves the current checkout root. Bare managed gates are not
// supported by the lean guard.
func FindGitRoot(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = abs
	cmd.Env = NonInteractiveEnv(abs)
	winproc.Harden(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", abs)
	}
	root := strings.TrimSpace(string(out))
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		return resolved, nil
	}
	return root, nil
}

// GetRemoteURL returns Git's effective URL for a named remote.
func GetRemoteURL(ctx context.Context, dir, name string) (string, error) {
	return Run(ctx, dir, "remote", "get-url", name)
}

// RemoteEndpoint binds provider policy to the literal configured origin while
// binding network reads/writes to Git's single effective push destination.
type RemoteEndpoint struct {
	FetchLiteral  string
	PushLiteral   string
	PushEffective string
}

// ResolveOriginEndpoint rejects ambiguous multiple push URLs. Callers can
// validate PushLiteral as the same provider as FetchLiteral and then use the
// immutable PushEffective URL instead of the mutable remote name for delivery.
func ResolveOriginEndpoint(ctx context.Context, dir string) (RemoteEndpoint, error) {
	fetchLiteral, err := Run(ctx, dir, "config", "--get", "remote.origin.url")
	if err != nil || strings.TrimSpace(fetchLiteral) == "" {
		return RemoteEndpoint{}, fmt.Errorf("origin remote is missing")
	}
	pushLiteral := strings.TrimSpace(fetchLiteral)
	if configured, configuredErr := Run(ctx, dir, "config", "--get-all", "remote.origin.pushurl"); configuredErr == nil && strings.TrimSpace(configured) != "" {
		values := nonEmptyLines(configured)
		if len(values) != 1 {
			return RemoteEndpoint{}, fmt.Errorf("origin has %d configured push URLs; exactly one is required", len(values))
		}
		pushLiteral = values[0]
	}
	effective, err := Run(ctx, dir, "remote", "get-url", "--push", "--all", "origin")
	if err != nil {
		return RemoteEndpoint{}, fmt.Errorf("resolve effective origin push URL: %w", err)
	}
	values := nonEmptyLines(effective)
	if len(values) != 1 {
		return RemoteEndpoint{}, fmt.Errorf("origin resolves to %d effective push URLs; exactly one is required", len(values))
	}
	return RemoteEndpoint{FetchLiteral: strings.TrimSpace(fetchLiteral), PushLiteral: pushLiteral, PushEffective: values[0]}, nil
}

func nonEmptyLines(value string) []string {
	var lines []string
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// HeadSHA returns the immutable commit currently named by HEAD.
func HeadSHA(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "rev-parse", "HEAD")
}

// CurrentBranch returns the attached branch name or HEAD when detached.
func CurrentBranch(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
}

const boundEndpointRemote = "_no_mistakes_bound_endpoint_"

func boundEndpointEnv(url string) []string {
	return []string{
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=remote." + boundEndpointRemote + ".url",
		"GIT_CONFIG_VALUE_0=" + url,
		"GIT_CONFIG_KEY_1=remote." + boundEndpointRemote + ".pushurl",
		"GIT_CONFIG_VALUE_1=" + url,
	}
}

// LsRemoteEndpoint inspects a previously resolved endpoint without putting
// credential-bearing URLs in argv or consulting mutable repository remotes.
func LsRemoteEndpoint(ctx context.Context, dir, endpoint, ref string) (string, error) {
	out, err := runRawWithEnv(ctx, dir, boundEndpointEnv(endpoint), "ls-remote", boundEndpointRemote, ref)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], nil
}

// FetchEndpoint fetches exactly ref from a bound endpoint into FETCH_HEAD.
func FetchEndpoint(ctx context.Context, dir, endpoint, ref string) error {
	_, err := runRawWithEnv(ctx, dir, boundEndpointEnv(endpoint), "fetch", "--no-tags", boundEndpointRemote, ref)
	return err
}

// PushCommitEndpoint pushes an immutable commit to a bound endpoint with no
// force mode and no credential-bearing URL in argv.
func PushCommitEndpoint(ctx context.Context, dir, endpoint, commitSHA, ref string) error {
	_, err := runRawWithEnv(ctx, dir, boundEndpointEnv(endpoint), "push", boundEndpointRemote, commitSHA+":"+ref)
	return err
}

// LsRemote returns the SHA for exactly ref, or an empty string when absent.
func LsRemote(ctx context.Context, dir, remote, ref string) (string, error) {
	out, err := Run(ctx, dir, "ls-remote", remote, ref)
	if err != nil || out == "" {
		return "", err
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], nil
}
