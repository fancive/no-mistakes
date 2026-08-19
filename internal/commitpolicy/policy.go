// Package commitpolicy owns the provider-specific rules for authoring the
// user's initial commit. Pipeline-authored fix commits remain governed by the
// pipeline's separate commit configuration.
package commitpolicy

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kunchenguid/no-mistakes/internal/conventional"
	"github.com/kunchenguid/no-mistakes/internal/scm"
)

var (
	icodeSubjectPattern  = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_]*-[0-9]+) \[(Story|Bug|Task)\] (.+)$`)
	icodeChangeIDPattern = regexp.MustCompile(`(?m)^Change-Id:\s+I[0-9a-fA-F]{40}\s*$`)
	coAuthorPattern      = regexp.MustCompile(`(?im)^\s*co-authored-by\s*:`)
	aiAttributionPattern = regexp.MustCompile(`(?im)^\s*(?:generated\s+with\s+(?:claude(?:\s+code)?|codex|chatgpt|copilot|ai)|ai[- ]generated(?:-by)?\s*:?)`)
)

// Author is an explicit Git commit author identity.
type Author struct {
	Name  string
	Email string
}

// AuthorFor returns the author override required by provider. Providers not
// listed here preserve the repository's configured Git author.
func AuthorFor(provider scm.Provider) (Author, bool) {
	if provider == scm.ProviderGitHub {
		return Author{Name: "fancivez", Email: "fancive@gmail.com"}, true
	}
	return Author{}, false
}

// ValidateMessage enforces the provider's subject convention and the common
// ban on agent attribution trailers.
func ValidateMessage(provider scm.Provider, message string) error {
	if !utf8.ValidString(message) {
		return fmt.Errorf("commit message must contain valid UTF-8")
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return fmt.Errorf("commit message must not be empty")
	}
	if strings.ContainsRune(message, '\x00') {
		return fmt.Errorf("commit message must not contain NUL bytes")
	}
	if coAuthorPattern.MatchString(message) || aiAttributionPattern.MatchString(message) {
		return fmt.Errorf("commit message must not contain Co-Authored-By or AI attribution lines")
	}

	subject, _, _ := strings.Cut(message, "\n")
	subject = strings.TrimSpace(subject)
	switch provider {
	case scm.ProviderGitHub:
		if !conventional.IsTitle(subject) {
			return fmt.Errorf("GitHub commit subject must use conventional format: type(scope): description")
		}
	case scm.ProviderICode:
		match := icodeSubjectPattern.FindStringSubmatch(subject)
		if len(match) == 0 {
			return fmt.Errorf("iCode commit subject must use iCafe format: {icafe-id} [Story|Bug|Task] {中文描述}")
		}
		if !containsHan(match[3]) {
			return fmt.Errorf("iCode iCafe commit description must contain Chinese text")
		}
	}
	return nil
}

// ValidatePath rejects files that the shared submission rules never allow in
// an authored commit.
func ValidatePath(_ scm.Provider, path string) error {
	base := strings.ToLower(filepath.Base(filepath.FromSlash(path)))
	switch {
	case base == ".env" || strings.HasPrefix(base, ".env."):
		return fmt.Errorf("sensitive file %q must not be staged", path)
	case base == ".npmrc" || base == ".netrc" || base == ".pypirc":
		return fmt.Errorf("sensitive registry or network config %q must not be staged", path)
	case strings.HasPrefix(base, "credentials"):
		return fmt.Errorf("sensitive credentials file %q must not be staged", path)
	case strings.Contains(base, "secret"):
		return fmt.Errorf("sensitive secret file %q must not be staged", path)
	case base == "id_rsa" || base == "id_ed25519" || base == "id_ecdsa":
		return fmt.Errorf("sensitive private key %q must not be staged", path)
	case strings.HasSuffix(base, ".pem"), strings.HasSuffix(base, ".key"),
		strings.HasSuffix(base, ".p12"), strings.HasSuffix(base, ".pfx"),
		strings.HasSuffix(base, ".jks"), strings.HasSuffix(base, ".keystore"):
		return fmt.Errorf("sensitive key file %q must not be staged", path)
	}
	return nil
}

// HasValidICodeChangeID reports whether a commit message contains the Gerrit
// Change-Id footer required to keep later pipeline fixes on one iCode review.
func HasValidICodeChangeID(message string) bool {
	return icodeChangeIDPattern.MatchString(message)
}

func containsHan(value string) bool {
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
