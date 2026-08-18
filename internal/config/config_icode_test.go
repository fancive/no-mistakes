package config

import (
	"strings"
	"testing"
)

func TestLoadRepoICodePolicy(t *testing.T) {
	t.Parallel()

	cfg, err := LoadRepoFromBytes([]byte(`icode:
  auto_submit: true
  reviewers:
    - reviewer1
    - reviewer.two
`))
	if err != nil {
		t.Fatalf("LoadRepoFromBytes() error = %v", err)
	}
	if !cfg.ICode.AutoSubmit || len(cfg.ICode.Reviewers) != 2 || cfg.ICode.Reviewers[1] != "reviewer.two" {
		t.Fatalf("icode config = %+v", cfg.ICode)
	}
}

func TestLoadRepoICodePolicyRejectsInvalidReviewers(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"icode:\n  reviewers: ['']\n",
		"icode:\n  reviewers: ['bad user']\n",
		"icode:\n  reviewers: ['same', 'SAME']\n",
	} {
		if _, err := LoadRepoFromBytes([]byte(raw)); err == nil || !strings.Contains(err.Error(), "icode.reviewers") {
			t.Fatalf("LoadRepoFromBytes(%q) error = %v, want reviewer validation", raw, err)
		}
	}
}

func TestEffectiveRepoConfigICodePolicyIsTrustedOnly(t *testing.T) {
	t.Parallel()

	pushed := &RepoConfig{ICode: ICodeRaw{AutoSubmit: true, Reviewers: []string{"pushed"}}}
	trusted := &RepoConfig{ICode: ICodeRaw{AutoSubmit: false, Reviewers: []string{"trusted"}}}
	effective := EffectiveRepoConfig(pushed, trusted, true)
	if effective.ICode.AutoSubmit || len(effective.ICode.Reviewers) != 1 || effective.ICode.Reviewers[0] != "trusted" {
		t.Fatalf("effective iCode policy = %+v, want trusted policy", effective.ICode)
	}
	withoutTrusted := EffectiveRepoConfig(pushed, nil, true)
	if withoutTrusted.ICode.AutoSubmit || len(withoutTrusted.ICode.Reviewers) != 0 {
		t.Fatalf("effective iCode policy without trusted copy = %+v, want safe defaults", withoutTrusted.ICode)
	}
}

func TestMergeICodePolicyDefaultsAndOverrides(t *testing.T) {
	t.Parallel()

	defaults := Merge(DefaultGlobalConfig(), &RepoConfig{})
	if defaults.ICode.AutoSubmit || strings.Join(defaults.ICode.Reviewers, ",") != "jipeng03,fanzheqiang" {
		t.Fatalf("default iCode policy = %+v", defaults.ICode)
	}
	overridden := Merge(DefaultGlobalConfig(), &RepoConfig{ICode: ICodeRaw{
		AutoSubmit: true,
		Reviewers:  []string{"reviewer1", "reviewer2"},
	}})
	if !overridden.ICode.AutoSubmit || strings.Join(overridden.ICode.Reviewers, ",") != "reviewer1,reviewer2" {
		t.Fatalf("overridden iCode policy = %+v", overridden.ICode)
	}
}
