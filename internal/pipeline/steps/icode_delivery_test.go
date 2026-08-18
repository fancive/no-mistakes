package steps

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/db"
	"github.com/kunchenguid/no-mistakes/internal/pipeline"
)

const testICodeChangeID = "I0123456789abcdef0123456789abcdef01234567"

func TestCommitPipelineChangesICodeAmendsTipAndPreservesChangeID(t *testing.T) {
	t.Parallel()

	dir := initICodeCommitRepo(t, true)
	beforeCount := gitCmd(t, dir, "rev-list", "--count", "HEAD")
	beforeHead := gitCmd(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "change.txt"), []byte("updated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "change.txt")
	sctx := &pipeline.StepContext{
		Ctx:     context.Background(),
		WorkDir: dir,
		Repo:    &db.Repo{UpstreamURL: "ssh://user@icode.baidu.com:8235/baidu/inputmethod/v5api"},
	}

	if err := commitPipelineChanges(sctx, "ignored fix subject"); err != nil {
		t.Fatalf("commitPipelineChanges() error = %v", err)
	}
	afterCount := gitCmd(t, dir, "rev-list", "--count", "HEAD")
	afterHead := gitCmd(t, dir, "rev-parse", "HEAD")
	message := gitCmd(t, dir, "log", "-1", "--format=%B")
	if afterCount != beforeCount {
		t.Fatalf("commit count = %s, want unchanged %s", afterCount, beforeCount)
	}
	if afterHead == beforeHead {
		t.Fatal("amend did not advance HEAD")
	}
	if !strings.Contains(message, "IMInput-1 [Story] feature") || !strings.Contains(message, "Change-Id: "+testICodeChangeID) {
		t.Fatalf("amended message lost subject or Change-Id: %q", message)
	}
}

func TestCommitPipelineChangesICodeRejectsMissingChangeID(t *testing.T) {
	t.Parallel()

	dir := initICodeCommitRepo(t, false)
	if err := os.WriteFile(filepath.Join(dir, "change.txt"), []byte("updated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "change.txt")
	sctx := &pipeline.StepContext{
		Ctx:     context.Background(),
		WorkDir: dir,
		Repo:    &db.Repo{UpstreamURL: "ssh://user@icode.baidu.com:8235/baidu/inputmethod/v5api"},
	}

	err := commitPipelineChanges(sctx, "ignored")
	if err == nil || !strings.Contains(err.Error(), "missing a valid Change-Id") {
		t.Fatalf("commitPipelineChanges() error = %v, want Change-Id refusal", err)
	}
	if got := gitCmd(t, dir, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("commit count = %s, want 1 after refusal", got)
	}
}

func TestICodeReviewRef(t *testing.T) {
	t.Parallel()

	got, err := icodeReviewRef("refs/heads/v5api_1-0-1814_BRANCH")
	if err != nil {
		t.Fatalf("icodeReviewRef() error = %v", err)
	}
	if got != "refs/for/v5api_1-0-1814_BRANCH" {
		t.Fatalf("icodeReviewRef() = %q", got)
	}
	for _, invalid := range []string{"", "refs/for/main", "main\nother"} {
		if _, err := icodeReviewRef(invalid); err == nil {
			t.Fatalf("icodeReviewRef(%q) accepted invalid target", invalid)
		}
	}
}

func TestICodeRebaseTargetsOnlyReleaseBranch(t *testing.T) {
	t.Parallel()

	got := icodeRebaseTargets("v5api_1-0-1814_BRANCH", "master", "origin/v5api_1-0-1814_BRANCH")
	if len(got) != 1 || got[0] != "origin/v5api_1-0-1814_BRANCH" {
		t.Fatalf("icodeRebaseTargets() = %v", got)
	}
	if got := icodeRebaseTargets("master", "master", "origin/master"); len(got) != 0 {
		t.Fatalf("default-branch targets = %v, want none", got)
	}
}

func TestPipelineBaseBranchUsesICodeReviewTarget(t *testing.T) {
	t.Parallel()

	sctx := &pipeline.StepContext{
		Ctx: context.Background(),
		Run: &db.Run{Branch: "refs/heads/v5api_1-0-1814_BRANCH"},
		Repo: &db.Repo{
			UpstreamURL:   "ssh://user@icode.baidu.com:8235/baidu/inputmethod/v5api",
			DefaultBranch: "master",
		},
	}
	if got := pipelineBaseBranch(sctx); got != "v5api_1-0-1814_BRANCH" {
		t.Fatalf("pipelineBaseBranch(iCode) = %q", got)
	}
	sctx.Repo.UpstreamURL = "https://github.com/test/repo"
	if got := pipelineBaseBranch(sctx); got != "master" {
		t.Fatalf("pipelineBaseBranch(GitHub) = %q", got)
	}
}

func initICodeCommitRepo(t *testing.T, withChangeID bool) string {
	t.Helper()
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.email", "test@example.com")
	gitCmd(t, dir, "config", "user.name", "Test")
	gitCmd(t, dir, "config", "core.hooksPath", ".git/no-hooks")
	if err := os.WriteFile(filepath.Join(dir, "change.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", "change.txt")
	message := "IMInput-1 [Story] feature"
	if withChangeID {
		message += "\n\nChange-Id: " + testICodeChangeID
	}
	gitCmd(t, dir, "commit", "-m", message)
	return dir
}
