package scm

import (
	"context"
	"testing"
)

func TestDetectSupportedProviders(t *testing.T) {
	tests := map[string]Provider{
		"git@github.com:fancive/repo.git":                 ProviderGitHub,
		"https://github.com/other/repo.git":               ProviderGitHub,
		"ssh://icode.baidu.com:8235/baidu/inputmethod/x":  ProviderICode,
		"https://icode.baidu.com/baidu/inputmethod/x.git": ProviderICode,
		"git@gitlab.com:group/repo.git":                   ProviderUnknown,
		"https://github.com.evil/owner/repo.git":          ProviderUnknown,
		"https://example.com/path/github.com/repo.git":    ProviderUnknown,
	}
	for remote, want := range tests {
		if got := DetectProviderContext(context.Background(), remote); got != want {
			t.Errorf("DetectProviderContext(%q) = %q, want %q", remote, got, want)
		}
	}
}
