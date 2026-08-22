package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBinaryVersionAndRemovedDaemonCommand(t *testing.T) {
	binary := buildTestBinary(t)
	if out, err := exec.Command(binary, "--version").CombinedOutput(); err != nil || !strings.Contains(string(out), "no-mistakes version") {
		t.Fatalf("--version: %v\n%s", err, out)
	}
	out, err := exec.Command(binary, "daemon").CombinedOutput()
	if err == nil {
		t.Fatalf("removed daemon command succeeded:\n%s", out)
	}
	for _, want := range []string{"schema_version: 1", "status: blocked", "stateless lean-guard migration"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("daemon output missing %q:\n%s", want, out)
		}
	}
}

func TestBinaryCheckJourneyIsReadOnlyAndChinese(t *testing.T) {
	binary := buildTestBinary(t)
	root := filepath.Join(t.TempDir(), "repo")
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	runGit(root, "init", "-b", "main")
	runGit(root, "config", "user.name", "Test")
	runGit(root, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(root, "add", "main.go")
	runGit(root, "commit", "-m", "feat: initial")
	runGit(filepath.Dir(remote), "init", "--bare", remote)
	runGit(remote, "symbolic-ref", "HEAD", "refs/heads/main")
	literalRemote := "git@github.com:fancive/lean-check-e2e.git"
	runGit(root, "remote", "add", "origin", literalRemote)
	runGit(root, "config", "url."+remote+".insteadOf", literalRemote)
	runGit(root, "push", "-u", "origin", "main")
	if err := os.WriteFile(filepath.Join(root, ".no-mistakes.yaml"), []byte("output_language: zh-CN\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n// change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := runGit(root, "rev-parse", "HEAD")
	cmd := exec.Command(binary, "check", "--file", "main.go", "--message", "feat(cli): 精简守卫")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("check: %v\n%s", err, out)
	}
	for _, want := range []string{"schema_version: 1", "status: passed", "output_language: zh-CN", "运行 no-mistakes commit"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("check output missing %q:\n%s", want, out)
		}
	}
	if after := runGit(root, "rev-parse", "HEAD"); after != before {
		t.Fatalf("read-only check changed HEAD: %s -> %s", before, after)
	}
	bad := exec.Command(binary, "check", "--file", "main.go", "--message", "not conventional")
	bad.Dir = root
	badOutput, badErr := bad.CombinedOutput()
	if badErr == nil {
		t.Fatalf("invalid check succeeded:\n%s", badOutput)
	}
	for _, want := range []string{"output_language: zh-CN", "error_code: commit_policy", "提交范围或提交信息不符合规则"} {
		if !strings.Contains(string(badOutput), want) {
			t.Errorf("Chinese blocker missing %q:\n%s", want, badOutput)
		}
	}
}

func buildTestBinary(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/no-mistakes"
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", path, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build test binary: %v\n%s", err, out)
	}
	return path
}
