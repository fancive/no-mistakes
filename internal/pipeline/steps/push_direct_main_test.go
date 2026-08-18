package steps

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kunchenguid/no-mistakes/internal/config"
)

func TestPushStepFanciveRepositoryFastForwardsDefaultBranchWithoutPRRef(t *testing.T) {
	t.Parallel()

	upstream := t.TempDir()
	gitCmd(t, upstream, "init", "--bare")

	dir, baseSHA, headSHA := setupGitRepo(t)
	gitCmd(t, dir, "remote", "add", "origin", upstream)
	gitCmd(t, dir, "push", "origin", "refs/heads/main:refs/heads/main")

	sctx := newTestContextWithDBRecords(t, &mockAgent{name: "test"}, dir, baseSHA, headSHA, config.Commands{})
	sctx.Repo.UpstreamURL = "git@github.com:fancive/example.git"
	sctx.Repo.DefaultBranch = "main"
	sctx.Run.Branch = "refs/heads/feature"
	recordReviewApproval(t, sctx, headSHA)

	_, err := (&PushStep{}).Execute(sctx)
	if err != nil {
		t.Fatalf("PushStep.Execute() error = %v", err)
	}
	if got := gitCmd(t, upstream, "rev-parse", "refs/heads/main"); got != headSHA {
		t.Fatalf("remote main = %s, want %s", got, headSHA)
	}
	featureCheck := exec.Command("git", "--git-dir="+upstream, "show-ref", "--verify", "refs/heads/feature")
	if err := featureCheck.Run(); err == nil {
		t.Fatal("direct-main delivery unexpectedly created feature ref")
	}
	run, err := sctx.DB.GetRun(sctx.Run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if run.PushRef == nil || *run.PushRef != "refs/heads/main" {
		t.Fatalf("persisted push ref = %v, want refs/heads/main", run.PushRef)
	}
	if run.PushTargetKind == nil || *run.PushTargetKind != "direct-main" {
		t.Fatalf("persisted target kind = %v, want direct-main", run.PushTargetKind)
	}
}

func TestPushStepFanciveRepositoryRefusesNonFastForwardDefaultBranch(t *testing.T) {
	t.Parallel()

	upstream := t.TempDir()
	gitCmd(t, upstream, "init", "--bare")
	dir, baseSHA, headSHA := setupGitRepo(t)
	gitCmd(t, dir, "remote", "add", "origin", upstream)
	gitCmd(t, dir, "push", "origin", "refs/heads/main:refs/heads/main")

	other := t.TempDir()
	gitCmd(t, other, "init")
	gitCmd(t, other, "config", "user.name", "Other")
	gitCmd(t, other, "config", "user.email", "other@example.com")
	gitCmd(t, other, "remote", "add", "origin", upstream)
	gitCmd(t, other, "fetch", "origin", "main")
	gitCmd(t, other, "checkout", "-b", "main", "FETCH_HEAD")
	if err := os.WriteFile(filepath.Join(other, "remote.txt"), []byte("remote\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, other, "add", "remote.txt")
	gitCmd(t, other, "commit", "-m", "remote advance")
	gitCmd(t, other, "push", "origin", "main")
	remoteHead := gitCmd(t, upstream, "rev-parse", "refs/heads/main")

	sctx := newTestContextWithDBRecords(t, &mockAgent{name: "test"}, dir, baseSHA, headSHA, config.Commands{})
	sctx.Repo.UpstreamURL = "https://github.com/fancive/example.git"
	sctx.Repo.DefaultBranch = "main"
	sctx.Run.Branch = "refs/heads/feature"
	recordReviewApproval(t, sctx, headSHA)

	_, err := (&PushStep{}).Execute(sctx)
	if err == nil || !strings.Contains(err.Error(), "non-fast-forward") {
		t.Fatalf("error = %v, want non-fast-forward refusal", err)
	}
	if got := gitCmd(t, upstream, "rev-parse", "refs/heads/main"); got != remoteHead {
		t.Fatalf("remote main changed from %s to %s", remoteHead, got)
	}
}
