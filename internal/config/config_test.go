package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRepoLeanSchema(t *testing.T) {
	root := t.TempDir()
	payload := "output_language: zh-CN\ncommands:\n  lint: bash scripts/lint-changed.sh\nicode:\n  auto_submit: true\n  reviewers: [alice, bob]\n"
	if err := os.WriteFile(filepath.Join(root, ".no-mistakes.yaml"), []byte(payload), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OutputLanguage != "zh-CN" || cfg.Commands.Lint == "" || !cfg.ICodeAutoSubmit() || len(cfg.ICode.Reviewers) != 2 {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestLoadRepoRejectsRemovedAndUnknownFields(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".no-mistakes.yaml"), []byte("agent: codex\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadRepo(root)
	if err == nil || !strings.Contains(err.Error(), "field agent not found") {
		t.Fatalf("LoadRepo error = %v", err)
	}
}

func TestLanguageUsesEnglishDefaultAndChineseOptIn(t *testing.T) {
	if got := (&RepoConfig{}).Language(); got != "en" {
		t.Fatalf("default language = %q", got)
	}
	if got := (&RepoConfig{OutputLanguage: "zh-CN"}).Language(); got != "zh-CN" {
		t.Fatalf("Chinese language = %q", got)
	}
}

func TestICodeAutoSubmitDefaultsOffAndCanBeEnabled(t *testing.T) {
	if (&RepoConfig{}).ICodeAutoSubmit() {
		t.Fatal("iCode auto-submit default is on")
	}
	enabled := true
	if !(&RepoConfig{ICode: ICodeRaw{AutoSubmit: &enabled}}).ICodeAutoSubmit() {
		t.Fatal("explicit iCode auto-submit true was ignored")
	}
}

func TestICodePolicyHashIsStableAndBindsEveryWriteSetting(t *testing.T) {
	enabled := true
	base := &RepoConfig{ICode: ICodeRaw{AutoSubmit: &enabled, Reviewers: []string{"alice", "bob"}}}
	if base.ICodePolicyHash("baidu/inputmethod/server", "server_BRANCH") == "" || base.ICodePolicyHash("baidu/inputmethod/server", "server_BRANCH") != base.ICodePolicyHash("baidu/inputmethod/server", "server_BRANCH") {
		t.Fatalf("unstable policy hash %q", base.ICodePolicyHash("baidu/inputmethod/server", "server_BRANCH"))
	}
	reordered := &RepoConfig{ICode: ICodeRaw{AutoSubmit: &enabled, Reviewers: []string{"bob", "alice"}}}
	if base.ICodePolicyHash("baidu/inputmethod/server", "server_BRANCH") != reordered.ICodePolicyHash("baidu/inputmethod/server", "server_BRANCH") {
		t.Fatal("reviewer order changed a set-valued policy")
	}
	disabled := false
	withoutSubmit := &RepoConfig{ICode: ICodeRaw{AutoSubmit: &disabled, Reviewers: []string{"alice", "bob"}}}
	if base.ICodePolicyHash("baidu/inputmethod/server", "server_BRANCH") == withoutSubmit.ICodePolicyHash("baidu/inputmethod/server", "server_BRANCH") {
		t.Fatal("auto_submit was not bound")
	}
	if base.ICodePolicyHash("baidu/inputmethod/server", "server_BRANCH") == base.ICodePolicyHash("baidu/inputmethod/other", "server_BRANCH") {
		t.Fatal("repository was not bound")
	}
	if base.ICodePolicyHash("baidu/inputmethod/server", "server_BRANCH") == base.ICodePolicyHash("baidu/inputmethod/server", "other_BRANCH") {
		t.Fatal("branch was not bound")
	}
}

func TestDetectOutputLanguageSurvivesOtherInvalidFields(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".no-mistakes.yaml"), []byte("output_language: zh-CN\nremoved_field: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DetectOutputLanguage(root); got != "zh-CN" {
		t.Fatalf("language = %q", got)
	}
}
