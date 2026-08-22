// Package github owns only the remote-owner routing needed for safe push.
package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/kunchenguid/no-mistakes/internal/git"
	"github.com/kunchenguid/no-mistakes/internal/scm"
)

// ForkLayout describes the conventional origin=fork, upstream=parent checkout.
type ForkLayout struct {
	UpstreamRemote string
	UpstreamURL    string
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
	return ForkLayout{UpstreamRemote: "upstream", UpstreamURL: upstreamURL}, true, nil
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
