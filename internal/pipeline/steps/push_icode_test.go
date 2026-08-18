package steps

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/config"
)

func TestPushStepICodeUsesRefsForAndPersistsReview(t *testing.T) {
	t.Parallel()

	dir, baseSHA, _ := setupGitRepo(t)
	gitCmd(t, dir, "config", "core.hooksPath", ".git/no-hooks")
	gitCmd(t, dir, "commit", "--amend", "-m", "IMInput-1 [Story] provider\n\nChange-Id: "+testICodeChangeID)
	headSHA := gitCmd(t, dir, "rev-parse", "HEAD")

	remote := filepath.Join(t.TempDir(), "icode.git")
	cmd := exec.Command("git", "init", "--bare", remote)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init --bare: %v: %s", err, out)
	}

	sctx := newTestContextWithDBRecords(t, &mockAgent{name: "codex"}, dir, baseSHA, headSHA, config.Commands{})
	sctx.Repo.UpstreamURL = "ssh://user@icode.baidu.com:8235/baidu/inputmethod/v5api"
	sctx.Repo.ForkURL = remote
	sctx.Run.Branch = "refs/heads/feature"
	sctx.Env = fakeICode(t, "feature", headSHA)
	recordReviewApproval(t, sctx, headSHA)

	outcome, err := (&PushStep{}).Execute(sctx)
	if err != nil {
		t.Fatalf("PushStep.Execute() error = %v", err)
	}
	wantURL := "https://console.cloud.baidu-int.com/devops/icode/repos/baidu/inputmethod/v5api/reviews/123/"
	if outcome == nil || outcome.PRURL != wantURL {
		t.Fatalf("PushStep outcome = %+v, want PR URL %q", outcome, wantURL)
	}
	remoteHead := gitCmd(t, dir, "--git-dir="+remote, "rev-parse", "refs/for/feature")
	if remoteHead != headSHA {
		t.Fatalf("refs/for/feature = %s, want %s", remoteHead, headSHA)
	}
	run, err := sctx.DB.GetRun(sctx.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.PRURL == nil || *run.PRURL != wantURL {
		t.Fatalf("persisted PR URL = %v, want %q", run.PRURL, wantURL)
	}
	if run.PushRef == nil || *run.PushRef != "refs/for/feature" {
		t.Fatalf("persisted pushed ref = %v, want refs/for/feature", run.PushRef)
	}
	if run.LastPushedSHA == nil || !strings.EqualFold(*run.LastPushedSHA, headSHA) {
		t.Fatalf("persisted pushed head = %v, want %s", run.LastPushedSHA, headSHA)
	}
}
