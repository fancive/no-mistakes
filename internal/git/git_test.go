package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	run("init", "-b", "main")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-m", "initial")
	return root
}

func TestRunAndRepositoryFacts(t *testing.T) {
	root := initRepo(t)
	ctx := context.Background()
	resolved, err := FindGitRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == "" {
		t.Fatal("empty root")
	}
	branch, err := CurrentBranch(ctx, root)
	if err != nil || branch != "main" {
		t.Fatalf("branch = %q, err = %v", branch, err)
	}
	sha, err := HeadSHA(ctx, root)
	if err != nil || len(sha) < 7 {
		t.Fatalf("sha = %q, err = %v", sha, err)
	}
}

func TestResolveOriginEndpointBindsSinglePushURL(t *testing.T) {
	root := initRepo(t)
	ctx := context.Background()
	if _, err := Run(ctx, root, "remote", "add", "origin", "git@github.com:upstream/repo.git"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, root, "config", "remote.origin.pushurl", "git@github.com:fork/repo.git"); err != nil {
		t.Fatal(err)
	}
	endpoint, err := ResolveOriginEndpoint(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if endpoint.FetchLiteral != "git@github.com:upstream/repo.git" || endpoint.FetchEffective != endpoint.FetchLiteral ||
		endpoint.PushLiteral != "git@github.com:fork/repo.git" || endpoint.PushEffective != endpoint.PushLiteral {
		t.Fatalf("endpoint = %+v", endpoint)
	}
	if _, err := Run(ctx, root, "config", "--add", "remote.origin.pushurl", "git@github.com:other/repo.git"); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveOriginEndpoint(ctx, root); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("ambiguous push URLs error = %v", err)
	}
}

func TestBoundEndpointPushAndRead(t *testing.T) {
	root := initRepo(t)
	remote := filepath.Join(t.TempDir(), "endpoint.git")
	if output, err := exec.Command("git", "init", "--bare", remote).CombinedOutput(); err != nil {
		t.Fatalf("init bare: %v\n%s", err, output)
	}
	if output, err := exec.Command("git", "--git-dir="+remote, "symbolic-ref", "HEAD", "refs/heads/main").CombinedOutput(); err != nil {
		t.Fatalf("set bare HEAD: %v\n%s", err, output)
	}
	ctx := context.Background()
	sha, _ := HeadSHA(ctx, root)
	if err := PushCommitEndpoint(ctx, root, remote, sha, "refs/heads/main"); err != nil {
		t.Fatal(err)
	}
	got, err := LsRemoteEndpoint(ctx, root, remote, "refs/heads/main")
	if err != nil || got != sha {
		t.Fatalf("endpoint SHA = %q, want %q, err = %v", got, sha, err)
	}
	symref, err := LsRemoteSymrefEndpoint(ctx, root, remote, "HEAD")
	if err != nil || !strings.Contains(symref, "ref: refs/heads/main\tHEAD") {
		t.Fatalf("endpoint symref = %q, err = %v", symref, err)
	}
}
