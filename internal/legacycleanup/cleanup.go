// Package legacycleanup removes only hash-confirmed state owned by the retired
// daemon/gate architecture. Planning is read-only and confirmation re-plans.
package legacycleanup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

const planVersion = 1

// Repository is the minimal legacy database mapping needed to remove an exact
// no-mistakes remote without retaining the SQLite runtime.
type Repository struct {
	ID          string `json:"id"`
	WorkingPath string `json:"working_path"`
}

// Run is the minimal legacy row needed to prove an exact managed worktree.
type Run struct {
	ID     string `json:"id"`
	RepoID string `json:"repo_id"`
	Status string `json:"status"`
}

// State is the read-only legacy database projection.
type State struct {
	Repositories []Repository
	Runs         []Run
	ActiveRuns   []string
}

// StateReader reads old state. The production reader shells out to sqlite3 so
// the new binary does not retain the SQLite Go dependency.
type StateReader interface {
	Read(context.Context, string) (State, error)
}

// Target is one hash-bound cleanup operation.
type Target struct {
	Kind        string `json:"kind"`
	Display     string `json:"display"`
	Path        string `json:"path,omitempty"`
	RepoRoot    string `json:"repo_root,omitempty"`
	RemoteName  string `json:"remote_name,omitempty"`
	ExpectedURL string `json:"expected_url,omitempty"`
	Fingerprint string `json:"fingerprint"`
}

// Plan is canonical, read-only cleanup evidence.
type Plan struct {
	Version  int      `json:"version"`
	Root     string   `json:"root"`
	Targets  []Target `json:"targets"`
	Blockers []string `json:"blockers"`
	Hash     string   `json:"hash"`
}

// Receipt records exact targets removed in one confirmed invocation.
type Receipt struct {
	PlanHash string
	Removed  []string
}

// Options configures the migration-only cleanup service.
type Options struct {
	Root           string
	Reader         StateReader
	ProcessAlive   func(int) bool
	SocketActive   func(string) (bool, error)
	ServiceRemover func(context.Context, string) error
	ServiceFiles   []string
}

// Service carries only immutable configuration; Plan always discovers live state.
type Service struct {
	root           string
	reader         StateReader
	processAlive   func(int) bool
	socketActive   func(string) (bool, error)
	serviceRemover func(context.Context, string) error
	serviceFiles   []string
}

// New constructs a cleanup service. Root defaults to NM_HOME or ~/.no-mistakes.
func New(options Options) *Service {
	root := strings.TrimSpace(options.Root)
	if root == "" {
		root = strings.TrimSpace(os.Getenv("NM_HOME"))
	}
	if root == "" {
		if home, err := os.UserHomeDir(); err == nil {
			root = filepath.Join(home, ".no-mistakes")
		}
	}
	reader := options.Reader
	if reader == nil {
		reader = sqliteCLIReader{}
	}
	alive := options.ProcessAlive
	if alive == nil {
		alive = processAlive
	}
	socketActive := options.SocketActive
	if socketActive == nil {
		socketActive = legacySocketActive
	}
	serviceFiles := append([]string(nil), options.ServiceFiles...)
	serviceRemover := options.ServiceRemover
	if options.ServiceFiles == nil {
		serviceFiles = defaultServiceFiles(root)
		if serviceRemover == nil {
			serviceRemover = removeLegacyService
		}
	} else if serviceRemover == nil {
		serviceRemover = func(context.Context, string) error { return nil }
	}
	return &Service{root: root, reader: reader, processAlive: alive, socketActive: socketActive, serviceRemover: serviceRemover, serviceFiles: serviceFiles}
}

// Plan inventories exact owned state without creating, deleting, or changing it.
func (s *Service) Plan(ctx context.Context) (Plan, error) {
	root, err := canonicalPath(s.root)
	if err != nil {
		return Plan{}, fmt.Errorf("resolve legacy root: %w", err)
	}
	if root == string(filepath.Separator) || root == "." || root == "" {
		return Plan{}, fmt.Errorf("refuse unsafe legacy root %q", root)
	}
	if unsafeBroadRoot(root) {
		return Plan{}, fmt.Errorf("refuse broad legacy root %q; expected a dedicated no-mistakes state directory", root)
	}
	plan := Plan{Version: planVersion, Root: root}

	dbPath := filepath.Join(root, "state.sqlite")
	state, stateErr := s.reader.Read(ctx, dbPath)
	if stateErr != nil && pathExists(dbPath) {
		plan.Blockers = append(plan.Blockers, "legacy database state is uncertain: "+stateErr.Error())
	}
	for _, run := range state.ActiveRuns {
		plan.Blockers = append(plan.Blockers, "legacy run is still active: "+run)
	}
	if pid, present, pidErr := readPID(filepath.Join(root, "daemon.pid")); pidErr != nil {
		plan.Blockers = append(plan.Blockers, "legacy daemon pid is uncertain: "+pidErr.Error())
	} else if present && s.processAlive(pid) {
		plan.Blockers = append(plan.Blockers, fmt.Sprintf("legacy daemon process %d is still active", pid))
	}
	socketPath := filepath.Join(root, "socket")
	if pathExists(socketPath) {
		active, socketErr := s.socketActive(socketPath)
		switch {
		case socketErr != nil:
			plan.Blockers = append(plan.Blockers, "legacy daemon socket state is uncertain: "+socketErr.Error())
		case active:
			plan.Blockers = append(plan.Blockers, "legacy daemon socket is still accepting connections")
		}
	}

	for _, item := range []struct{ kind, path string }{
		{"database", dbPath},
		{"database", dbPath + "-wal"},
		{"database", dbPath + "-shm"},
		{"daemon-state", filepath.Join(root, "socket")},
		{"daemon-state", filepath.Join(root, "daemon.pid")},
		{"daemon-state", filepath.Join(root, "daemon.lock")},
	} {
		target, blocker, ok := ownedPathTarget(root, item.kind, item.path)
		if blocker != "" {
			plan.Blockers = append(plan.Blockers, blocker)
		}
		if ok {
			plan.Targets = append(plan.Targets, target)
		}
	}
	s.inventoryManagedReposAndRuns(root, state, &plan)

	for _, path := range s.serviceFiles {
		if !pathExists(path) {
			continue
		}
		if info, statErr := os.Lstat(path); statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			plan.Blockers = append(plan.Blockers, "legacy service definition is not a regular owned file: "+path)
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			plan.Blockers = append(plan.Blockers, "legacy service definition is unreadable: "+path)
			continue
		}
		text := string(content)
		configuredRoot := filepath.Clean(s.root)
		if (!strings.Contains(text, root) && !strings.Contains(text, configuredRoot)) || !strings.Contains(text, "daemon") {
			plan.Blockers = append(plan.Blockers, "service file ownership is uncertain: "+path)
			continue
		}
		fingerprint, err := fingerprintPath(path)
		if err != nil {
			plan.Blockers = append(plan.Blockers, "fingerprint service file: "+err.Error())
			continue
		}
		plan.Targets = append(plan.Targets, Target{Kind: "service", Display: path, Path: path, Fingerprint: fingerprint})
	}
	platformTargets, platformBlockers := discoverLegacyPlatformServices(ctx, root)
	plan.Targets = append(plan.Targets, platformTargets...)
	plan.Blockers = append(plan.Blockers, platformBlockers...)

	for _, repo := range state.Repositories {
		working, err := canonicalPath(repo.WorkingPath)
		if err != nil || !pathExists(working) {
			continue
		}
		gate := filepath.Join(root, "repos", repo.ID+".git")
		remoteURL, err := configuredRemoteURL(ctx, working, "no-mistakes")
		if err != nil {
			continue
		}
		remoteCanonical, err := canonicalPath(remoteURL)
		if err != nil || remoteCanonical != gate {
			continue
		}
		fingerprint := hashString(working + "\x00no-mistakes\x00" + gate)
		plan.Targets = append(plan.Targets, Target{
			Kind: "remote", Display: working + "#no-mistakes", RepoRoot: working,
			RemoteName: "no-mistakes", ExpectedURL: gate, Fingerprint: fingerprint,
		})
	}

	sort.Strings(plan.Blockers)
	sort.Slice(plan.Targets, func(i, j int) bool {
		left, right := targetOrder(plan.Targets[i].Kind), targetOrder(plan.Targets[j].Kind)
		if left != right {
			return left < right
		}
		return plan.Targets[i].Display < plan.Targets[j].Display
	})
	plan.Hash, err = hashPlan(plan)
	if err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func targetOrder(kind string) int {
	switch kind {
	case "remote":
		return 0
	case "service":
		return 1
	case "scheduled-task":
		return 1
	case "worktree":
		return 2
	case "gates":
		return 3
	case "database":
		return 4
	case "daemon-state":
		return 5
	default:
		return 100
	}
}

func (s *Service) inventoryManagedReposAndRuns(root string, state State, plan *Plan) {
	for _, dir := range []string{filepath.Join(root, "repos"), filepath.Join(root, "worktrees")} {
		if blocker := inspectManagedContainer(dir); blocker != "" {
			plan.Blockers = append(plan.Blockers, blocker)
		}
	}
	expectedRepos := make(map[string]struct{}, len(state.Repositories))
	expectedRuns := make(map[string]map[string]struct{})
	for _, repo := range state.Repositories {
		if !validManagedID(repo.ID) {
			plan.Blockers = append(plan.Blockers, "legacy repository id is unsafe: "+repo.ID)
			continue
		}
		expectedRepos[repo.ID] = struct{}{}
		path := filepath.Join(root, "repos", repo.ID+".git")
		if target, blocker, ok := ownedPathTarget(root, "gates", path); blocker != "" {
			plan.Blockers = append(plan.Blockers, blocker)
		} else if ok {
			plan.Targets = append(plan.Targets, target)
		}
	}
	for _, run := range state.Runs {
		if !validManagedID(run.ID) || !validManagedID(run.RepoID) {
			plan.Blockers = append(plan.Blockers, "legacy run identity is unsafe: "+run.RepoID+"/"+run.ID)
			continue
		}
		if _, ok := expectedRepos[run.RepoID]; !ok {
			plan.Blockers = append(plan.Blockers, "legacy run references unknown repository: "+run.RepoID+"/"+run.ID)
			continue
		}
		switch strings.ToLower(strings.TrimSpace(run.Status)) {
		case "completed", "failed", "cancelled":
		default:
			plan.Blockers = append(plan.Blockers, "legacy run status is not proven terminal: "+run.RepoID+"/"+run.ID+" ("+run.Status+")")
		}
		if expectedRuns[run.RepoID] == nil {
			expectedRuns[run.RepoID] = make(map[string]struct{})
		}
		expectedRuns[run.RepoID][run.ID] = struct{}{}
		path := filepath.Join(root, "worktrees", run.RepoID, run.ID)
		if target, blocker, ok := ownedPathTarget(root, "worktree", path); blocker != "" {
			plan.Blockers = append(plan.Blockers, blocker)
		} else if ok {
			plan.Targets = append(plan.Targets, target)
		}
	}
	checkUnexpectedEntries(filepath.Join(root, "repos"), func(name string) bool {
		if !strings.HasSuffix(name, ".git") {
			return false
		}
		_, ok := expectedRepos[strings.TrimSuffix(name, ".git")]
		return ok
	}, "legacy gates", plan)
	checkUnexpectedEntries(filepath.Join(root, "worktrees"), func(name string) bool {
		_, ok := expectedRepos[name]
		return ok
	}, "legacy worktree repositories", plan)
	for repoID := range expectedRepos {
		if blocker := inspectManagedContainer(filepath.Join(root, "worktrees", repoID)); blocker != "" {
			plan.Blockers = append(plan.Blockers, blocker)
		}
		allowed := expectedRuns[repoID]
		checkUnexpectedEntries(filepath.Join(root, "worktrees", repoID), func(name string) bool {
			_, ok := allowed[name]
			return ok
		}, "legacy worktrees for "+repoID, plan)
	}
}

func inspectManagedContainer(path string) string {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		return "cannot inspect legacy managed root: " + err.Error()
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "managed path is a symlink and will not be removed: " + path
	}
	if !info.IsDir() {
		return "managed path is not a directory and will not be removed: " + path
	}
	return ""
}

func validManagedID(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "." && value != ".." && filepath.Base(value) == value && !strings.ContainsAny(value, `/\\`)
}

func checkUnexpectedEntries(dir string, allowed func(string) bool, label string, plan *Plan) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		plan.Blockers = append(plan.Blockers, "cannot inspect "+label+": "+err.Error())
		return
	}
	for _, entry := range entries {
		if !allowed(entry.Name()) {
			plan.Blockers = append(plan.Blockers, "unowned entry under "+label+": "+filepath.Join(dir, entry.Name()))
		}
	}
}

func unsafeBroadRoot(root string) bool {
	home, err := os.UserHomeDir()
	if err == nil && home != "" {
		home, _ = canonicalPath(home)
		rel, relErr := filepath.Rel(root, home)
		if relErr == nil && (rel == "." || rel == "" || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return true
		}
	}
	return false
}

// Confirm re-plans, requires the exact hash and no blockers, then removes each
// target only after a final target-local fingerprint check.
func (s *Service) Confirm(ctx context.Context, expectedHash string) (Receipt, error) {
	plan, err := s.Plan(ctx)
	if err != nil {
		return Receipt{}, err
	}
	if len(plan.Blockers) > 0 {
		return Receipt{}, fmt.Errorf("legacy cleanup is blocked: %s", strings.Join(plan.Blockers, "; "))
	}
	if strings.TrimSpace(expectedHash) == "" || expectedHash != plan.Hash {
		return Receipt{}, fmt.Errorf("legacy cleanup plan hash changed: expected %s, current %s", expectedHash, plan.Hash)
	}
	receipt := Receipt{PlanHash: plan.Hash}
	for _, target := range plan.Targets {
		if err := s.applyTarget(ctx, plan.Root, target); err != nil {
			return receipt, fmt.Errorf("remove %s: %w", target.Display, err)
		}
		receipt.Removed = append(receipt.Removed, target.Kind+":"+target.Display)
	}
	return receipt, nil
}

func ownedPathTarget(root, kind, path string) (Target, string, bool) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return Target{}, "", false
	}
	if err != nil {
		return Target{}, "inspect managed path: " + err.Error(), false
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Target{}, "managed path is a symlink and will not be removed: " + path, false
	}
	if !insideRoot(root, path) {
		return Target{}, "managed path escapes legacy root: " + path, false
	}
	if !physicalInsideRoot(root, path) {
		return Target{}, "managed path physically escapes legacy root: " + path, false
	}
	fingerprint, err := fingerprintPath(path)
	if err != nil {
		return Target{}, "fingerprint managed path: " + err.Error(), false
	}
	return Target{Kind: kind, Display: path, Path: path, Fingerprint: fingerprint}, "", true
}

func (s *Service) applyTarget(ctx context.Context, root string, target Target) error {
	if target.Kind == "scheduled-task" {
		return removeLegacyPlatformService(ctx, target)
	}
	if target.Kind == "remote" {
		current, err := configuredRemoteURL(ctx, target.RepoRoot, target.RemoteName)
		if err != nil {
			return err
		}
		canonical, err := canonicalPath(current)
		if err != nil || canonical != target.ExpectedURL {
			return fmt.Errorf("remote target changed")
		}
		if hashString(target.RepoRoot+"\x00"+target.RemoteName+"\x00"+canonical) != target.Fingerprint {
			return fmt.Errorf("remote fingerprint changed")
		}
		cmd := exec.CommandContext(ctx, "git", "remote", "remove", target.RemoteName)
		cmd.Dir = target.RepoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git remote remove: %s: %w", strings.TrimSpace(string(out)), err)
		}
		return nil
	}
	if target.Kind != "service" && !insideRoot(root, target.Path) {
		return fmt.Errorf("path escapes legacy root")
	}
	if target.Kind != "service" && !physicalInsideRoot(root, target.Path) {
		return fmt.Errorf("path physically escapes legacy root")
	}
	current, err := fingerprintPath(target.Path)
	if err != nil {
		return err
	}
	if current != target.Fingerprint {
		return fmt.Errorf("target fingerprint changed")
	}
	if target.Kind == "service" {
		if err := s.serviceRemover(ctx, target.Path); err != nil {
			return fmt.Errorf("unregister legacy service: %w", err)
		}
	}
	return os.RemoveAll(target.Path)
}

func fingerprintPath(path string) (string, error) {
	var entries []string
	err := filepath.WalkDir(path, func(current string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(path, current)
		if err != nil {
			return err
		}
		line := filepath.ToSlash(rel) + "\x00" + info.Mode().String() + "\x00" + strconv.FormatInt(info.Size(), 10) + "\x00" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(current)
			if err != nil {
				return err
			}
			line += "\x00" + link
		} else if info.Mode().IsRegular() {
			file, err := os.Open(current)
			if err != nil {
				return err
			}
			contentHash := sha256.New()
			_, copyErr := io.Copy(contentHash, file)
			closeErr := file.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
			line += "\x00" + hex.EncodeToString(contentHash.Sum(nil))
		}
		entries = append(entries, line)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hashString(strings.Join(entries, "\n")), nil
}

func hashPlan(plan Plan) (string, error) {
	plan.Hash = ""
	payload, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("encode cleanup plan: %w", err)
	}
	return hashString(string(payload)), nil
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func canonicalPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return filepath.Clean(abs), nil
}

func insideRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func physicalInsideRoot(root, path string) bool {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func configuredRemoteURL(ctx context.Context, repo, name string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "config", "--get", "remote."+name+".url")
	cmd.Dir = repo
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func readPID(path string) (pid int, present bool, err error) {
	payload, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, true, err
	}
	var record struct {
		PID int `json:"pid"`
	}
	if json.Unmarshal(payload, &record) == nil && record.PID > 0 {
		return record.PID, true, nil
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(payload)))
	if err != nil || pid <= 0 {
		return 0, true, fmt.Errorf("invalid daemon pid file")
	}
	return pid, true, nil
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func defaultServiceFiles(root string) []string {
	home, _ := os.UserHomeDir()
	suffix := hashString(filepath.Clean(root))[:8]
	switch runtime.GOOS {
	case "darwin":
		base := filepath.Join(home, "Library", "LaunchAgents")
		return []string{
			filepath.Join(base, "com.kunchenguid.no-mistakes.daemon."+suffix+".plist"),
			filepath.Join(base, "com.kunchenguid.no-mistakes.daemon.plist"),
		}
	case "linux":
		base := filepath.Join(home, ".config", "systemd", "user")
		return []string{
			filepath.Join(base, "no-mistakes-daemon-"+suffix+".service"),
			filepath.Join(base, "no-mistakes-daemon.service"),
		}
	default:
		return nil
	}
}

type sqliteCLIReader struct{}

func (sqliteCLIReader) Read(ctx context.Context, dbPath string) (State, error) {
	if !pathExists(dbPath) {
		return State{}, nil
	}
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return State{}, fmt.Errorf("sqlite3 is required to inspect legacy state: %w", err)
	}
	var repos []Repository
	if err := sqliteJSON(ctx, dbPath, `SELECT id, working_path FROM repos ORDER BY id`, &repos); err != nil {
		return State{}, err
	}
	var runs []Run
	if err := sqliteJSON(ctx, dbPath, `SELECT id, repo_id, status FROM runs ORDER BY id`, &runs); err != nil {
		return State{}, err
	}
	state := State{Repositories: repos, Runs: runs}
	for _, run := range runs {
		if run.Status == "pending" || run.Status == "running" {
			state.ActiveRuns = append(state.ActiveRuns, run.ID)
		}
	}
	return state, nil
}

func sqliteJSON(ctx context.Context, dbPath, query string, destination any) error {
	cmd := exec.CommandContext(ctx, "sqlite3", "-readonly", "-json", dbPath, query)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("inspect legacy sqlite state: %s: %w", strings.TrimSpace(string(out)), err)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		out = []byte("[]")
	}
	if err := json.Unmarshal(out, destination); err != nil {
		return fmt.Errorf("decode legacy sqlite state: %w", err)
	}
	return nil
}
