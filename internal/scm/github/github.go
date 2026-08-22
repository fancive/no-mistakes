// Package github owns only the remote-owner routing needed for safe push.
package github

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/kunchenguid/no-mistakes/internal/git"
	"github.com/kunchenguid/no-mistakes/internal/scm"
)

// ForkLayout describes the conventional origin=fork, upstream=parent checkout.
type ForkLayout struct {
	UpstreamRemote       string
	UpstreamURL          string
	UpstreamEffectiveURL string
}

// RepoSlug extracts the first owner/name pair from HTTP, SSH URL, or scp form.
func RepoSlug(remote string) string {
	value := strings.TrimSpace(strings.TrimSuffix(remote, ".git"))
	if marker := strings.Index(value, "://"); marker >= 0 {
		value = value[marker+3:]
		if slash := strings.IndexByte(value, '/'); slash >= 0 {
			value = value[slash+1:]
		} else {
			return ""
		}
	} else if colon := strings.IndexByte(value, ':'); colon >= 0 {
		value = value[colon+1:]
	}
	parts := strings.Split(strings.Trim(value, "/"), "/")
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return ""
	}
	return strings.TrimSpace(parts[0]) + "/" + strings.TrimSpace(parts[1])
}

// DirectMainRemote is the explicit personal-owner exception. All other
// GitHub owners receive feature-branch push only.
func DirectMainRemote(remote string) bool {
	owner, _, ok := strings.Cut(RepoSlug(remote), "/")
	return ok && strings.EqualFold(strings.TrimSpace(owner), "fancive")
}

// SameRepository compares owner/name case-insensitively across URL forms.
func SameRepository(left, right string) bool {
	leftSlug, rightSlug := RepoSlug(left), RepoSlug(right)
	return leftSlug != "" && strings.EqualFold(leftSlug, rightSlug)
}

// ForkOf reports whether fork and upstream have the same repository name under
// different GitHub owners.
func ForkOf(fork, upstream string) bool {
	forkOwner, forkName, forkOK := strings.Cut(RepoSlug(fork), "/")
	upstreamOwner, upstreamName, upstreamOK := strings.Cut(RepoSlug(upstream), "/")
	return forkOK && upstreamOK &&
		!strings.EqualFold(forkOwner, upstreamOwner) &&
		strings.EqualFold(forkName, upstreamName)
}

// NetworkEndpoint moves only default-port github.com SSH URLs onto GitHub's
// SSH-over-443 endpoint. Literal remotes remain the provider/policy authority;
// this derived endpoint is used only for bound network reads and writes. An
// explicitly configured non-default port or non-GitHub transport is preserved.
func NetworkEndpoint(remote string) string {
	value := strings.TrimSpace(remote)
	lower := strings.ToLower(value)
	defaultSSH := strings.HasPrefix(lower, "git@github.com:")
	if strings.HasPrefix(lower, "ssh://") {
		parsed, err := url.Parse(value)
		if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") {
			return value
		}
		port := parsed.Port()
		defaultSSH = port == "" || port == "22"
	}
	if !defaultSSH {
		return value
	}
	slug := RepoSlug(value)
	if slug == "" {
		return value
	}
	return "ssh://git@ssh.github.com:443/" + slug + ".git"
}

// DetectForkLayout recognizes the conventional GitHub fork checkout without
// changing remotes. An ambiguous upstream remote fails closed so a personal
// fork can never be mistaken for a direct-main repository.
func DetectForkLayout(ctx context.Context, root, originURL string) (ForkLayout, bool, error) {
	remotes, err := git.Run(ctx, root, "remote")
	if err != nil {
		return ForkLayout{}, false, fmt.Errorf("list Git remotes: %w", err)
	}
	found := false
	for _, remote := range strings.Fields(remotes) {
		if remote == "upstream" {
			found = true
			break
		}
	}
	if !found {
		return ForkLayout{}, false, nil
	}
	configured, err := git.Run(ctx, root, "config", "--get-all", "remote.upstream.url")
	if err != nil {
		return ForkLayout{}, false, fmt.Errorf("read upstream remote: %w", err)
	}
	urls := strings.Fields(configured)
	if len(urls) != 1 {
		return ForkLayout{}, false, fmt.Errorf("upstream has %d configured URLs; exactly one is required", len(urls))
	}
	upstreamURL := urls[0]
	if scm.DetectProviderContext(ctx, upstreamURL) != scm.ProviderGitHub || !ForkOf(originURL, upstreamURL) {
		return ForkLayout{}, false, nil
	}
	effective, err := git.Run(ctx, root, "remote", "get-url", "--all", "upstream")
	if err != nil {
		return ForkLayout{}, false, fmt.Errorf("resolve effective upstream URL: %w", err)
	}
	effectiveURLs := strings.Fields(effective)
	if len(effectiveURLs) != 1 {
		return ForkLayout{}, false, fmt.Errorf("upstream resolves to %d effective URLs; exactly one is required", len(effectiveURLs))
	}
	return ForkLayout{
		UpstreamRemote: "upstream", UpstreamURL: upstreamURL, UpstreamEffectiveURL: effectiveURLs[0],
	}, true, nil
}

// FeatureTrackingRef returns the configured short upstream ref for an attached
// GitHub feature branch. An empty result means the branch has no upstream.
func FeatureTrackingRef(ctx context.Context, root, branch string) (string, error) {
	out, err := git.Run(ctx, root, "for-each-ref", "--format=%(upstream:short)", "refs/heads/"+branch)
	if err != nil {
		return "", fmt.Errorf("resolve feature tracking ref: %w", err)
	}
	return strings.TrimSpace(out), nil
}
