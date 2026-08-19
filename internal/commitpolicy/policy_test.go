package commitpolicy

import (
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/scm"
)

func TestValidateMessageGitHubRequiresConventionalTitle(t *testing.T) {
	if err := ValidateMessage(scm.ProviderGitHub, "feat(commit): add provider-aware staging"); err != nil {
		t.Fatalf("valid GitHub message rejected: %v", err)
	}
	if err := ValidateMessage(scm.ProviderGitHub, "add provider-aware staging"); err == nil {
		t.Fatal("non-conventional GitHub message accepted")
	}
}

func TestValidateMessageICodeRequiresICafeSubjectWithChineseDescription(t *testing.T) {
	valid := []string{
		"IMInput-10207 [Story] 增加资源闭环开关",
		"BAIDUINPUTBUG-217498 [Bug] 修复空配置响应",
		"InputServer-7140 [Task] 整理发布配置\n\n保留现有兼容逻辑。",
	}
	for _, message := range valid {
		if err := ValidateMessage(scm.ProviderICode, message); err != nil {
			t.Errorf("valid iCode message %q rejected: %v", message, err)
		}
	}

	invalid := []string{
		"feat(v5api): add resource loop switch",
		"IMInput-10207 [Feature] 增加资源闭环开关",
		"IMInput-10207 [Story] add resource loop switch",
		"[Story] 增加资源闭环开关",
	}
	for _, message := range invalid {
		if err := ValidateMessage(scm.ProviderICode, message); err == nil {
			t.Errorf("invalid iCode message %q accepted", message)
		}
	}
}

func TestValidateMessageRejectsAIAttributionForEveryProvider(t *testing.T) {
	providers := []scm.Provider{scm.ProviderGitHub, scm.ProviderICode, scm.ProviderUnknown}
	messages := []string{
		"feat(commit): add policy\n\nCo-Authored-By: Claude <noreply@example.com>",
		"IMInput-10207 [Story] 增加提交门禁\n\nGenerated with Claude Code",
		"plain message\n\nGenerated with Codex",
	}
	for i, provider := range providers {
		if err := ValidateMessage(provider, messages[i]); err == nil {
			t.Errorf("provider %s accepted AI attribution", provider)
		}
	}
}

func TestValidatePathRejectsSensitiveFilesAndAllowsDependencyFiles(t *testing.T) {
	for _, path := range []string{
		".env", ".npmrc", ".netrc", ".pypirc",
		"config/credentials-prod.json", "config/api-secret.yaml",
		"keys/id_rsa", "keys/id_ed25519", "keys/id_ecdsa",
		"tls/client.pem", "tls/client.key", "tls/client.p12", "tls/client.pfx",
		"tls/client.jks", "tls/client.keystore",
	} {
		if err := ValidatePath(scm.ProviderGitHub, path); err == nil {
			t.Errorf("sensitive path %q accepted", path)
		}
	}
	for _, path := range []string{"go.mod", "app/v5api/go.sum"} {
		if err := ValidatePath(scm.ProviderICode, path); err != nil {
			t.Errorf("iCode dependency path %q unexpectedly rejected: %v", path, err)
		}
	}
	if err := ValidatePath(scm.ProviderGitHub, "app/v5api/go.mod"); err != nil {
		t.Fatalf("GitHub go.mod unexpectedly rejected: %v", err)
	}
}

func TestAuthorPolicy(t *testing.T) {
	author, ok := AuthorFor(scm.ProviderGitHub)
	if !ok {
		t.Fatal("GitHub author policy missing")
	}
	if got := author.Name + " <" + author.Email + ">"; got != "fancivez <fancive@gmail.com>" {
		t.Fatalf("GitHub author = %q", got)
	}
	if _, ok := AuthorFor(scm.ProviderICode); ok {
		t.Fatal("iCode should preserve the repository's local author")
	}
}

func TestHasValidICodeChangeID(t *testing.T) {
	valid := "IMInput-10207 [Story] 增加提交门禁\n\nChange-Id: I" + strings.Repeat("a", 40)
	if !HasValidICodeChangeID(valid) {
		t.Fatal("valid Change-Id not detected")
	}
	if HasValidICodeChangeID("Change-Id: I1234") {
		t.Fatal("short Change-Id accepted")
	}
}
