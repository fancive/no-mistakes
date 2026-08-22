// Package commitprep creates the user's initial commit through an explicit,
// provider-aware staging boundary.
package commitprep

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/kunchenguid/no-mistakes/internal/commitpolicy"
	"github.com/kunchenguid/no-mistakes/internal/git"
	"github.com/kunchenguid/no-mistakes/internal/scm"
)

// Options describes one exact-scope commit.
type Options struct {
	Dir     string
	Files   []string
	Message string
	Amend   bool
}

// Result is the immutable evidence returned after a successful commit.
type Result struct {
	SHA      string
	Branch   string
	Provider scm.Provider
	Files    []string
	Amended  bool
}

// Validation is read-only evidence that the exact authored scope and message
// satisfy provider policy at one observed repository state. Commit always
// repeats this validation before mutation.
type Validation struct {
	Root     string
	Branch   string
	Provider scm.Provider
	Files    []string
	Message  string
}

// Validate performs the non-mutating half of Commit: repository/branch,
// message, paths, Gerrit hook, and pre-existing index-scope checks.
func Validate(ctx context.Context, opts Options) (Validation, error) {
	dir := opts.Dir
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	root, err := git.FindGitRoot(dir)
	if err != nil {
		return Validation{}, fmt.Errorf("find git root: %w", err)
	}
	branch, err := git.CurrentBranch(ctx, root)
	if err != nil {
		return Validation{}, fmt.Errorf("read current branch: %w", err)
	}
	if branch == "HEAD" {
		return Validation{}, fmt.Errorf("detached HEAD: create or switch to a feature branch before committing")
	}
	provider := detectProvider(ctx, root)
	message := strings.TrimSpace(opts.Message)
	newMessage := message != ""
	if opts.Amend {
		currentMessage, err := git.Run(ctx, root, "show", "-s", "--format=%B", "HEAD")
		if err != nil {
			return Validation{}, fmt.Errorf("read HEAD commit message: %w", err)
		}
		if !newMessage {
			message = currentMessage
		}
		if provider == scm.ProviderICode && newMessage {
			currentChangeID := commitpolicy.ICodeChangeID(currentMessage)
			if currentChangeID != "" {
				if id := commitpolicy.ICodeChangeID(message); id != "" {
					if id != currentChangeID {
						return Validation{}, fmt.Errorf("amended iCode message changes the current Change-Id footer; keep %s to stay on the current CR", currentChangeID)
					}
				} else {
					message += "\n\nChange-Id: " + currentChangeID
				}
			}
		}
	}
	if err := commitpolicy.ValidateMessage(provider, message); err != nil {
		return Validation{}, err
	}
	files, err := normalizeFiles(ctx, root, provider, opts.Files)
	if err != nil {
		return Validation{}, err
	}
	if provider == scm.ProviderICode {
		if err := requireCommitMsgHook(ctx, root); err != nil {
			return Validation{}, err
		}
	}
	stagedBefore, err := stagedFiles(ctx, root)
	if err != nil {
		return Validation{}, fmt.Errorf("inspect staged files: %w", err)
	}
	if extras := difference(stagedBefore, files); len(extras) > 0 {
		return Validation{}, fmt.Errorf("staged files outside the explicit --file list: %s", strings.Join(extras, ", "))
	}
	return Validation{Root: root, Branch: branch, Provider: provider, Files: files, Message: message}, nil
}

// Commit validates policy before mutating the index, stages only Files, checks
// that the complete index matches that list, and creates one commit, or with
// Amend set, amends the current commit in place (preserving an existing iCode
// Change-Id when the message is reused or replaced).
func Commit(ctx context.Context, opts Options) (Result, error) {
	dir := opts.Dir
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	root, err := git.FindGitRoot(dir)
	if err != nil {
		return Result{}, fmt.Errorf("find git root: %w", err)
	}
	branch, err := git.CurrentBranch(ctx, root)
	if err != nil {
		return Result{}, fmt.Errorf("read current branch: %w", err)
	}
	if branch == "HEAD" {
		return Result{}, fmt.Errorf("detached HEAD: create or switch to a feature branch before committing")
	}
	provider := detectProvider(ctx, root)
	message := strings.TrimSpace(opts.Message)
	newMessage := message != ""
	var preAmendChangeID string
	if opts.Amend {
		currentMessage, err := git.Run(ctx, root, "show", "-s", "--format=%B", "HEAD")
		if err != nil {
			return Result{}, fmt.Errorf("read HEAD commit message: %w", err)
		}
		if !newMessage {
			message = currentMessage
		}
		if provider == scm.ProviderICode {
			preAmendChangeID = commitpolicy.ICodeChangeID(currentMessage)
			if newMessage && preAmendChangeID != "" {
				if id := commitpolicy.ICodeChangeID(message); id != "" {
					if id != preAmendChangeID {
						return Result{}, fmt.Errorf("amended iCode message changes the current Change-Id footer; keep %s to stay on the current CR", preAmendChangeID)
					}
				} else {
					message += "\n\nChange-Id: " + preAmendChangeID
				}
			}
		}
	}
	if err := commitpolicy.ValidateMessage(provider, message); err != nil {
		return Result{}, err
	}
	files, err := normalizeFiles(ctx, root, provider, opts.Files)
	if err != nil {
		return Result{}, err
	}
	if provider == scm.ProviderICode {
		if err := requireCommitMsgHook(ctx, root); err != nil {
			return Result{}, err
		}
	}

	stagedBefore, err := stagedFiles(ctx, root)
	if err != nil {
		return Result{}, fmt.Errorf("inspect staged files: %w", err)
	}
	if extras := difference(stagedBefore, files); len(extras) > 0 {
		return Result{}, fmt.Errorf("staged files outside the explicit --file list: %s", strings.Join(extras, ", "))
	}

	snapshot, err := captureIndex(ctx, root)
	if err != nil {
		return Result{}, fmt.Errorf("snapshot git index: %w", err)
	}
	restoreIndex := true
	defer func() {
		if restoreIndex {
			_ = snapshot.restore()
		}
	}()

	stagedDeletions, err := stagedDeletedFiles(ctx, root)
	if err != nil {
		return Result{}, fmt.Errorf("inspect staged deletions: %w", err)
	}
	deleted := make(map[string]struct{}, len(stagedDeletions))
	for _, path := range stagedDeletions {
		deleted[path] = struct{}{}
	}
	filesToStage := make([]string, 0, len(files))
	for _, path := range files {
		if _, alreadyDeleted := deleted[path]; alreadyDeleted {
			if _, statErr := os.Lstat(filepath.Join(root, filepath.FromSlash(path))); os.IsNotExist(statErr) {
				// The exact deletion is already represented in the index. Passing
				// an absent path that is absent from the index back to git add makes
				// Git reject the otherwise valid literal pathspec.
				continue
			}
		}
		filesToStage = append(filesToStage, path)
	}
	if len(filesToStage) > 0 {
		// -A is scoped by the exact literal path list and captures selected
		// deletions as well as additions and modifications.
		addArgs := append([]string{"--literal-pathspecs", "add", "-A", "--"}, filesToStage...)
		if _, err := git.Run(ctx, root, addArgs...); err != nil {
			return Result{}, fmt.Errorf("stage explicit files: %w", err)
		}
	}
	staged, err := stagedFiles(ctx, root)
	if err != nil {
		return Result{}, fmt.Errorf("verify staged files: %w", err)
	}
	if !sameFiles(staged, files) {
		return Result{}, fmt.Errorf("staged files do not exactly match --file list (requested: %s; staged: %s)", strings.Join(files, ", "), strings.Join(staged, ", "))
	}
	if len(staged) == 0 {
		return Result{}, fmt.Errorf("no changes to commit")
	}

	beforeHead, err := git.Run(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return Result{}, fmt.Errorf("read HEAD before commit: %w", err)
	}
	commitArgs := []string{"commit"}
	if opts.Amend {
		commitArgs = append(commitArgs, "--amend")
	}
	if author, ok := commitpolicy.AuthorFor(provider); ok {
		commitArgs = append(commitArgs, "--author="+author.Name+" <"+author.Email+">")
	}
	if opts.Amend && !newMessage {
		commitArgs = append(commitArgs, "--no-edit")
	} else {
		commitArgs = append(commitArgs, "-m", message)
	}
	if _, err := git.Run(ctx, root, commitArgs...); err != nil {
		return Result{}, fmt.Errorf("create commit: %w", err)
	}
	restoreIndex = false

	sha, err := git.Run(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return Result{}, fmt.Errorf("read committed HEAD: %w", err)
	}
	if provider == scm.ProviderICode {
		committedMessage, readErr := git.Run(ctx, root, "show", "-s", "--format=%B", sha)
		reason := ""
		switch {
		case readErr != nil || !commitpolicy.HasValidICodeChangeID(committedMessage):
			reason = "iCode commit-msg hook did not add a valid Change-Id footer; fix the hook before committing"
		case opts.Amend && preAmendChangeID != "" && commitpolicy.ICodeChangeID(committedMessage) != preAmendChangeID:
			reason = fmt.Sprintf("amended iCode commit changed the Change-Id footer from %s; the commit-msg hook must preserve it", preAmendChangeID)
		}
		if reason != "" {
			_, resetErr := git.Run(ctx, root, "reset", "--mixed", beforeHead)
			_ = snapshot.restore()
			if resetErr != nil {
				return Result{}, fmt.Errorf("%s and rollback failed: %v", reason, resetErr)
			}
			return Result{}, fmt.Errorf("%s", reason)
		}
	}

	return Result{SHA: sha, Branch: branch, Provider: provider, Files: files, Amended: opts.Amend}, nil
}

func detectProvider(ctx context.Context, root string) scm.Provider {
	remote, err := git.Run(ctx, root, "config", "--get", "remote.origin.url")
	if err != nil || strings.TrimSpace(remote) == "" {
		remote, err = git.GetRemoteURL(ctx, root, "origin")
	}
	if err != nil {
		return scm.ProviderUnknown
	}
	return scm.DetectProviderContext(ctx, remote)
}

func normalizeFiles(ctx context.Context, root string, provider scm.Provider, values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one explicit --file is required")
	}
	seen := make(map[string]struct{}, len(values))
	files := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			return nil, fmt.Errorf("--file must not be empty")
		}
		if filepath.IsAbs(value) {
			return nil, fmt.Errorf("--file must be repository-relative: %q", value)
		}
		clean := filepath.Clean(value)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("--file must name a file inside the repository: %q", value)
		}
		path := filepath.ToSlash(clean)
		if _, duplicate := seen[path]; duplicate {
			return nil, fmt.Errorf("duplicate --file path: %q", path)
		}
		if err := commitpolicy.ValidatePath(provider, path); err != nil {
			return nil, err
		}
		full := filepath.Join(root, clean)
		info, err := os.Lstat(full)
		switch {
		case err == nil && info.IsDir():
			return nil, fmt.Errorf("--file must name an individual file, not a directory: %q", path)
		case err != nil && !os.IsNotExist(err):
			return nil, fmt.Errorf("inspect --file %q: %w", path, err)
		case os.IsNotExist(err):
			if _, trackedErr := git.Run(ctx, root, "--literal-pathspecs", "ls-files", "--error-unmatch", "--", path); trackedErr != nil {
				// A deletion that is already staged is absent from the index, so
				// ls-files cannot prove that it was tracked. Accept it only when
				// the exact literal path exists in HEAD.
				trackedAtHead, headErr := git.RunRaw(ctx, root, "--literal-pathspecs", "ls-tree", "--name-only", "-z", "HEAD", "--", path)
				want := append([]byte(path), 0)
				if headErr != nil || !bytes.Equal(trackedAtHead, want) {
					return nil, fmt.Errorf("--file does not exist and is not a tracked deletion: %q", path)
				}
			}
		}
		seen[path] = struct{}{}
		files = append(files, path)
	}
	sort.Strings(files)
	return files, nil
}

func stagedFiles(ctx context.Context, root string) ([]string, error) {
	out, err := git.RunRaw(ctx, root, "diff", "--cached", "--name-only", "--no-renames", "-z")
	return parseNULPaths(out, err)
}

func stagedDeletedFiles(ctx context.Context, root string) ([]string, error) {
	out, err := git.RunRaw(ctx, root, "diff", "--cached", "--name-only", "--no-renames", "--diff-filter=D", "-z")
	return parseNULPaths(out, err)
}

func parseNULPaths(out []byte, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	if out[len(out)-1] != 0 {
		return nil, fmt.Errorf("parse staged files: git returned a non-NUL-terminated path list")
	}
	rawParts := bytes.Split(out[:len(out)-1], []byte{0})
	parts := make([]string, 0, len(rawParts))
	for _, raw := range rawParts {
		if len(raw) == 0 {
			return nil, fmt.Errorf("parse staged files: git returned an empty path")
		}
		parts = append(parts, filepath.ToSlash(string(raw)))
	}
	sort.Strings(parts)
	return parts, nil
}

func difference(left, right []string) []string {
	wanted := make(map[string]struct{}, len(right))
	for _, path := range right {
		wanted[path] = struct{}{}
	}
	var extras []string
	for _, path := range left {
		if _, ok := wanted[path]; !ok {
			extras = append(extras, path)
		}
	}
	sort.Strings(extras)
	return extras
}

func sameFiles(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func requireCommitMsgHook(ctx context.Context, root string) error {
	hook, err := commitMsgHookPath(ctx, root)
	if err != nil {
		return err
	}
	info, err := os.Stat(hook)
	if err != nil || info.IsDir() {
		return fmt.Errorf("iCode requires an installed commit-msg hook before committing: %s", hook)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("iCode commit-msg hook is not executable: %s", hook)
	}
	return nil
}

func commitMsgHookPath(ctx context.Context, root string) (string, error) {
	hooksPath, err := git.Run(ctx, root, "config", "--path", "--get", "core.hooksPath")
	if err == nil && strings.TrimSpace(hooksPath) != "" {
		if !filepath.IsAbs(hooksPath) {
			hooksPath = filepath.Join(root, hooksPath)
		}
		return filepath.Join(hooksPath, "commit-msg"), nil
	}
	gitPath, err := git.Run(ctx, root, "rev-parse", "--git-path", "hooks/commit-msg")
	if err != nil {
		return "", fmt.Errorf("resolve iCode commit-msg hook: %w", err)
	}
	if !filepath.IsAbs(gitPath) {
		gitPath = filepath.Join(root, gitPath)
	}
	return filepath.Clean(gitPath), nil
}

type indexSnapshot struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
}

// IndexSnapshot is an opaque exact copy of the caller's Git index. It lets a
// deterministic pre-commit linter run without retaining incidental staging.
type IndexSnapshot struct{ snapshot indexSnapshot }

// CaptureIndex snapshots the repository index for later exact restoration.
func CaptureIndex(ctx context.Context, root string) (IndexSnapshot, error) {
	snapshot, err := captureIndex(ctx, root)
	if err != nil {
		return IndexSnapshot{}, err
	}
	return IndexSnapshot{snapshot: snapshot}, nil
}

// Restore restores the exact index bytes and mode captured earlier.
func (s IndexSnapshot) Restore() error { return s.snapshot.restore() }

func captureIndex(ctx context.Context, root string) (indexSnapshot, error) {
	path, err := git.Run(ctx, root, "rev-parse", "--git-path", "index")
	if err != nil {
		return indexSnapshot{}, err
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return indexSnapshot{path: path}, nil
	}
	if err != nil {
		return indexSnapshot{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return indexSnapshot{}, err
	}
	return indexSnapshot{path: path, data: data, mode: info.Mode().Perm(), exists: true}, nil
}

func (s indexSnapshot) restore() error {
	if !s.exists {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".no-mistakes-index-rollback-")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(s.data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(s.mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}
