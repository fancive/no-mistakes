// Package github owns only the remote-owner routing needed for safe push.
package github

import "strings"

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
