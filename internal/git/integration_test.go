package git_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mmo-con/git-dashboard/internal/domain"
	"github.com/mmo-con/git-dashboard/internal/git"
	tu "github.com/mmo-con/git-dashboard/internal/testutil"
)

func validate(t *testing.T, c *git.Client, path string) domain.Location {
	t.Helper()
	v := c.Validate(context.Background(), path)
	if v.Availability != domain.Available {
		t.Fatalf("validate %s: %v %v", path, v.Availability, v.Err)
	}
	return v.Location
}

func inspect(t *testing.T, c *git.Client, loc domain.Location) domain.Inspection {
	t.Helper()
	in, err := c.Inspect(context.Background(), loc, git.InspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func TestCheckVersion(t *testing.T) {
	tu.IsolateGit(t)
	if _, err := git.New("git").CheckVersion(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestInspectWorktreeStates(t *testing.T) {
	tu.IsolateGit(t)
	c := git.New("git")
	repo := tu.InitRepo(t, filepath.Join(tu.Canonical(t, t.TempDir()), "repo"))
	tu.Commit(t, repo, "a.txt", "a\n", "add a")
	tu.Commit(t, repo, "b.txt", "b\n", "add b")
	tu.Commit(t, repo, "c.txt", "c\n", "add c")
	loc := validate(t, c, repo)

	in := inspect(t, c, loc)
	if !in.Worktree.Valid || !in.Worktree.Value.Clean() {
		t.Fatalf("expected clean worktree: %+v", in.Worktree)
	}
	if in.LastCommit.Value == nil || in.LastCommit.Value.Subject != "add c" || in.LastCommit.Value.Author != "Test" {
		t.Fatalf("last commit = %+v", in.LastCommit.Value)
	}
	if !in.Remotes.Valid || len(in.Remotes.Value) != 0 {
		t.Fatalf("remotes = %+v", in.Remotes)
	}
	if in.FetchHead.Value != nil || !in.FetchHead.Valid {
		t.Fatalf("fetch head = %+v", in.FetchHead)
	}

	tu.WriteFile(t, filepath.Join(repo, "a.txt"), "changed\n")
	tu.WriteFile(t, filepath.Join(repo, "staged.txt"), "s\n")
	tu.Git(t, repo, "add", "staged.txt")
	os.Remove(filepath.Join(repo, "b.txt"))
	tu.Git(t, repo, "mv", "c.txt", "renamed.txt")
	tu.WriteFile(t, filepath.Join(repo, "untracked.txt"), "u\n")

	w := inspect(t, c, loc).Worktree.Value
	if w.Staged != 2 || w.Modified != 1 || w.Deleted != 1 || w.Renamed != 1 || w.Untracked != 1 || w.Conflicts != 0 {
		t.Fatalf("counts = %+v", w)
	}

	in, err := c.Inspect(context.Background(), loc, git.InspectOptions{Untracked: domain.UntrackedNo})
	if err != nil {
		t.Fatal(err)
	}
	if in.Worktree.Value.Untracked != 0 || in.Worktree.Value.UntrackedChecked {
		t.Fatalf("untracked=no: %+v", in.Worktree.Value)
	}
}

func TestInspectMergeConflictAndOperations(t *testing.T) {
	tu.IsolateGit(t)
	c := git.New("git")
	repo := tu.InitRepo(t, filepath.Join(tu.Canonical(t, t.TempDir()), "repo"))
	tu.Git(t, repo, "checkout", "-q", "-b", "other")
	tu.Commit(t, repo, "README", "other\n", "other")
	tu.Git(t, repo, "checkout", "-q", "main")
	tu.Commit(t, repo, "README", "main\n", "main")
	_, _ = tu.TryGit(repo, "merge", "other")
	loc := validate(t, c, repo)
	in := inspect(t, c, loc)
	if in.Operation.Value != domain.Merging || in.Worktree.Value.Conflicts != 1 {
		t.Fatalf("operation=%v worktree=%+v", in.Operation.Value, in.Worktree.Value)
	}
	tu.Git(t, repo, "merge", "--abort")

	tu.Git(t, repo, "bisect", "start")
	if got := inspect(t, c, loc).Operation.Value; got != domain.Bisecting {
		t.Fatalf("operation = %v, want bisecting", got)
	}
	tu.Git(t, repo, "bisect", "reset")

	_, _ = tu.TryGit(repo, "rebase", "other")
	if got := inspect(t, c, loc).Operation.Value; got != domain.Rebasing {
		t.Fatalf("operation = %v, want rebasing", got)
	}
}

func TestInspectHeadsAndUpstreams(t *testing.T) {
	tu.IsolateGit(t)
	c := git.New("git")
	base := tu.Canonical(t, t.TempDir())
	origin := tu.InitRepo(t, filepath.Join(base, "origin"))
	tu.Git(t, base, "clone", "-q", origin, "clone")
	clone := filepath.Join(base, "clone")
	loc := validate(t, c, clone)

	in := inspect(t, c, loc)
	u := in.Upstream.Value
	if !u.Configured || u.Ref != "origin/main" || u.Ahead != 0 || u.Behind != 0 {
		t.Fatalf("upstream = %+v", u)
	}
	if len(in.Remotes.Value) != 1 || in.Remotes.Value[0].URL != origin {
		t.Fatalf("remotes = %+v", in.Remotes.Value)
	}

	tu.Commit(t, origin, "o.txt", "o\n", "upstream change")
	tu.Git(t, clone, "fetch", "-q")
	u = inspect(t, c, loc).Upstream.Value
	if u.Behind != 1 || u.Ahead != 0 {
		t.Fatalf("behind: %+v", u)
	}
	tu.Commit(t, clone, "l.txt", "l\n", "local change")
	u = inspect(t, c, loc).Upstream.Value
	if !u.Diverged() {
		t.Fatalf("diverged: %+v", u)
	}

	tu.Git(t, clone, "checkout", "-q", "-b", "local-only")
	if in := inspect(t, c, loc); in.Upstream.Value.Configured || in.Head.Value.Branch != "local-only" {
		t.Fatalf("no upstream: %+v %+v", in.Head.Value, in.Upstream.Value)
	}

	tu.Git(t, origin, "branch", "feature")
	tu.Git(t, clone, "fetch", "-q")
	tu.Git(t, clone, "checkout", "-q", "-b", "feature", "--track", "origin/feature")
	tu.Git(t, origin, "branch", "-D", "feature")
	tu.Git(t, clone, "fetch", "-q", "--prune")
	if u := inspect(t, c, loc).Upstream.Value; !u.Gone {
		t.Fatalf("upstream gone: %+v", u)
	}

	tu.Git(t, clone, "checkout", "-q", "--detach", "HEAD")
	if h := inspect(t, c, loc).Head.Value; !h.Detached {
		t.Fatalf("detached: %+v", h)
	}

	tu.Git(t, clone, "remote", "add", "upstream", "https://user:secret@example.com/x.git")
	if r := inspect(t, c, loc).Remotes.Value; len(r) != 2 {
		t.Fatalf("multiple remotes: %+v", r)
	}

	unborn := filepath.Join(base, "unborn")
	tu.Git(t, "", "init", "-q", unborn)
	in = inspect(t, c, validate(t, c, unborn))
	if !in.Head.Value.Unborn || !in.LastCommit.Valid || in.LastCommit.Value != nil {
		t.Fatalf("unborn: %+v %+v", in.Head, in.LastCommit)
	}
}

func TestInspectOtherBranches(t *testing.T) {
	tu.IsolateGit(t)
	c := git.New("git")
	base := tu.Canonical(t, t.TempDir())
	origin := tu.InitRepo(t, filepath.Join(base, "origin"))
	tu.Git(t, origin, "branch", "gone")
	tu.Git(t, base, "clone", "-q", origin, "clone")
	clone := filepath.Join(base, "clone")
	tu.Git(t, clone, "branch", "--track", "gone", "origin/gone")
	tu.Git(t, origin, "branch", "-D", "gone")
	tu.Commit(t, origin, "o.txt", "o\n", "upstream change")
	tu.Git(t, clone, "fetch", "-q", "--prune")
	tu.Git(t, clone, "checkout", "-q", "-b", "feature")
	tu.Commit(t, clone, "f.txt", "f\n", "feature work")
	tu.Git(t, clone, "branch", "local-only")
	tu.Git(t, clone, "worktree", "add", "-q", "-b", "in-worktree", filepath.Join(base, "wt"))
	loc := validate(t, c, clone)

	in, err := c.Inspect(context.Background(), loc, git.InspectOptions{Branches: true})
	if err != nil {
		t.Fatal(err)
	}
	if in.LastCommit.Value == nil || in.LastCommit.Value.Subject != "feature work" || in.LastCommit.Value.Author != "Test" {
		t.Fatalf("last commit = %+v", in.LastCommit.Value)
	}
	got := map[string]domain.BranchState{}
	for _, b := range in.Branches.Value {
		got[b.Name] = b
	}
	if len(got) != 5 || !got["feature"].Current || !got["gone"].Gone || got["local-only"].Upstream != "" ||
		got["main"].Behind != 1 || got["main"].Upstream != "origin/main" || got["in-worktree"].Worktree == "" {
		t.Fatalf("branches = %+v", in.Branches.Value)
	}

	in, err = c.Inspect(context.Background(), loc, git.InspectOptions{})
	if err != nil || in.Branches.Valid || in.LastCommit.Value == nil || in.LastCommit.Value.Subject != "feature work" {
		t.Fatalf("without branches: %+v %+v %v", in.Branches, in.LastCommit, err)
	}

	tu.Git(t, clone, "checkout", "-q", "--detach", "HEAD~1")
	in, err = c.Inspect(context.Background(), loc, git.InspectOptions{Branches: true})
	if err != nil || in.LastCommit.Value == nil || in.LastCommit.Value.Subject != "initial commit" {
		t.Fatalf("detached: %+v %v", in.LastCommit, err)
	}
	if b := in.Branches.Value; len(b) != 5 {
		t.Fatalf("detached branches = %+v", b)
	}
}

func TestValidateClassification(t *testing.T) {
	tu.IsolateGit(t)
	c := git.New("git")
	base := tu.Canonical(t, t.TempDir())
	repo := tu.InitRepo(t, filepath.Join(base, "repo"))
	if err := os.MkdirAll(filepath.Join(repo, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	tu.Git(t, "", "init", "-q", "--bare", filepath.Join(base, "bare.git"))
	if err := os.MkdirAll(filepath.Join(base, "plain"), 0o755); err != nil {
		t.Fatal(err)
	}
	tu.Git(t, repo, "config", "--global", "safe.bareRepository", "explicit")

	tests := []struct {
		path string
		want domain.Availability
	}{
		{repo, domain.Available},
		{filepath.Join(repo, "sub"), domain.NotWorktreeRoot},
		{filepath.Join(base, "bare.git"), domain.BareRepository},
		{filepath.Join(base, "plain"), domain.NotARepository},
		{filepath.Join(base, "missing"), domain.Missing},
	}
	for _, tc := range tests {
		v := c.Validate(context.Background(), tc.path)
		if v.Availability != tc.want {
			t.Errorf("%s: got %v (%v), want %v", tc.path, v.Availability, v.Err, tc.want)
		}
	}
	if v := c.Validate(context.Background(), filepath.Join(repo, "sub")); !strings.Contains(v.Err.Error(), repo) {
		t.Errorf("error must name the worktree root: %v", v.Err)
	}
}

func TestLinkedWorktreesShareStore(t *testing.T) {
	tu.IsolateGit(t)
	c := git.New("git")
	base := tu.Canonical(t, t.TempDir())
	main := tu.InitRepo(t, filepath.Join(base, "main"))
	wt := filepath.Join(base, "wt")
	tu.Git(t, main, "worktree", "add", "-q", wt)

	a, b := validate(t, c, main), validate(t, c, wt)
	if a.CommonDir != b.CommonDir {
		t.Fatalf("common dirs differ: %s vs %s", a.CommonDir, b.CommonDir)
	}
	if a.Markers.FetchHead == b.Markers.FetchHead {
		t.Fatal("FETCH_HEAD must be resolved per worktree")
	}
	if b.WorktreeRoot != wt {
		t.Fatalf("worktree root = %s", b.WorktreeRoot)
	}
}

func TestInspectionTakesNoOptionalLocks(t *testing.T) {
	tu.IsolateGit(t)
	c := git.New("git")
	repo := tu.InitRepo(t, filepath.Join(tu.Canonical(t, t.TempDir()), "repo"))
	loc := validate(t, c, repo)
	index := filepath.Join(repo, ".git", "index")
	// Make the index stat information stale so that a normal git status would
	// refresh and rewrite it.
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(repo, "README"), future, future); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(index)
	time.Sleep(20 * time.Millisecond)
	inspect(t, c, loc)
	after, _ := os.Stat(index)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("inspection rewrote the index")
	}
}

func TestInheritedGitDirDoesNotRedirect(t *testing.T) {
	tu.IsolateGit(t)
	c := git.New("git")
	base := tu.Canonical(t, t.TempDir())
	a := tu.InitRepo(t, filepath.Join(base, "a"))
	b := tu.InitRepo(t, filepath.Join(base, "b"))
	tu.Git(t, b, "checkout", "-q", "-b", "branch-b")
	loc := validate(t, c, a)
	t.Setenv("GIT_DIR", filepath.Join(b, ".git"))
	t.Setenv("GIT_WORK_TREE", b)
	if h := inspect(t, c, loc).Head.Value; h.Branch != "main" {
		t.Fatalf("inspection was redirected to %q", h.Branch)
	}
}

func TestFetchAndFastForward(t *testing.T) {
	tu.IsolateGit(t)
	c := git.New("git")
	base := tu.Canonical(t, t.TempDir())
	origin := tu.InitRepo(t, filepath.Join(base, "origin"))
	tu.Git(t, base, "clone", "-q", origin, "clone")
	clone := filepath.Join(base, "clone")
	loc := validate(t, c, clone)

	tu.Commit(t, origin, "o.txt", "o\n", "upstream")
	if err := c.Fetch(context.Background(), loc, git.FetchOptions{Prune: true}); err != nil {
		t.Fatal(err)
	}
	in := inspect(t, c, loc)
	if in.Upstream.Value.Behind != 1 || in.FetchHead.Value == nil {
		t.Fatalf("after fetch: %+v fetch=%v", in.Upstream.Value, in.FetchHead.Value)
	}
	if err := c.MergeFastForwardOnly(context.Background(), loc); err != nil {
		t.Fatal(err)
	}
	if u := inspect(t, c, loc).Upstream.Value; u.Behind != 0 || u.Ahead != 0 {
		t.Fatalf("after merge: %+v", u)
	}

	tu.Commit(t, origin, "o2.txt", "o\n", "upstream 2")
	tu.Commit(t, clone, "l.txt", "l\n", "local")
	if err := c.Fetch(context.Background(), loc, git.FetchOptions{}); err != nil {
		t.Fatal(err)
	}
	head := tu.Git(t, clone, "rev-parse", "HEAD")
	if err := c.MergeFastForwardOnly(context.Background(), loc); err == nil {
		t.Fatal("divergence must be rejected")
	}
	if tu.Git(t, clone, "rev-parse", "HEAD") != head {
		t.Fatal("HEAD changed although the merge was rejected")
	}
	if code := git.ExitCode(c.MergeFastForwardOnly(context.Background(), loc)); code == nil || *code == 0 {
		t.Fatal("expected a non-zero exit code")
	}
}

func TestFastForwardKeepsIgnoredFiles(t *testing.T) {
	tu.IsolateGit(t)
	c := git.New("git")
	base := tu.Canonical(t, t.TempDir())
	origin := tu.InitRepo(t, filepath.Join(base, "origin"))
	tu.Commit(t, origin, ".gitignore", "local.conf\n", "ignore local.conf")
	tu.Git(t, base, "clone", "-q", origin, "clone")
	clone := filepath.Join(base, "clone")
	loc := validate(t, c, clone)

	tu.WriteFile(t, filepath.Join(origin, "local.conf"), "upstream\n")
	tu.Git(t, origin, "add", "-f", "local.conf")
	tu.Git(t, origin, "commit", "-q", "-m", "track local.conf")
	tu.WriteFile(t, filepath.Join(clone, "local.conf"), "local\n")
	if err := c.Fetch(context.Background(), loc, git.FetchOptions{}); err != nil {
		t.Fatal(err)
	}
	head := tu.Git(t, clone, "rev-parse", "HEAD")
	if err := c.MergeFastForwardOnly(context.Background(), loc); err == nil {
		t.Fatal("a fast-forward that overwrites an ignored file must fail")
	}
	if got, _ := os.ReadFile(filepath.Join(clone, "local.conf")); string(got) != "local\n" {
		t.Fatalf("ignored file was overwritten: %q", got)
	}
	if tu.Git(t, clone, "rev-parse", "HEAD") != head {
		t.Fatal("HEAD changed although the merge was rejected")
	}
}

// countingGit returns a client whose Git executable logs one line per process
// and then runs the real Git, and a function that returns and resets the count.
func countingGit(t *testing.T) (*git.Client, func() int) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	exe := filepath.Join(dir, "git")
	script := fmt.Sprintf("#!/bin/sh\necho x >> '%s'\nexec '%s' \"$@\"\n", log, real)
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return git.New(exe), func() int {
		data, _ := os.ReadFile(log)
		os.Remove(log)
		return strings.Count(string(data), "\n")
	}
}

// TestProcessBudget guards the cost model in docs/architecture.md: one
// validation process per candidate, and three processes per inspection, four
// for a detached HEAD. Startup time grows with the number of processes, so
// this replaces a wall-clock measurement that depends on the machine.
func TestProcessBudget(t *testing.T) {
	tu.IsolateGit(t)
	c, processes := countingGit(t)
	base := tu.Canonical(t, t.TempDir())
	origin := tu.InitRepo(t, filepath.Join(base, "origin"))
	tu.Git(t, base, "clone", "-q", origin, "clone")
	clone := filepath.Join(base, "clone")
	tu.Git(t, clone, "branch", "feature")

	loc := validate(t, c, clone)
	if n := processes(); n != 1 {
		t.Fatalf("validation started %d processes, want 1", n)
	}
	for _, branches := range []bool{true, false} {
		if _, err := c.Inspect(context.Background(), loc, git.InspectOptions{Branches: branches}); err != nil {
			t.Fatal(err)
		}
		if n := processes(); n != 3 {
			t.Fatalf("inspection (branches=%v) started %d processes, want 3", branches, n)
		}
	}
	tu.Git(t, clone, "checkout", "-q", "--detach")
	if _, err := c.Inspect(context.Background(), loc, git.InspectOptions{Branches: true}); err != nil {
		t.Fatal(err)
	}
	if n := processes(); n != 4 {
		t.Fatalf("inspection of a detached HEAD started %d processes, want 4", n)
	}
}

func TestFetchFailureCapturesStderr(t *testing.T) {
	tu.IsolateGit(t)
	c := git.New("git")
	repo := tu.InitRepo(t, filepath.Join(tu.Canonical(t, t.TempDir()), "repo"))
	tu.Git(t, repo, "remote", "add", "origin", filepath.Join(t.TempDir(), "does-not-exist"))
	err := c.Fetch(context.Background(), validate(t, c, repo), git.FetchOptions{})
	if err == nil || git.Stderr(err) == "" || git.ExitCode(err) == nil {
		t.Fatalf("err=%v stderr=%q", err, git.Stderr(err))
	}
}

// sshScript installs a fake ssh command and returns the remote URL that uses it.
func sshScript(t *testing.T, repo, body string) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "fake-ssh")
	tu.WriteFile(t, script, "#!/bin/sh\n"+body)
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_SSH_COMMAND", script)
	tu.Git(t, repo, "remote", "add", "origin", "ssh://example.invalid/repo.git")
}

func TestFetchCannotPromptOnTerminal(t *testing.T) {
	tu.IsolateGit(t)
	c := git.New("git")
	repo := tu.InitRepo(t, filepath.Join(tu.Canonical(t, t.TempDir()), "repo"))
	marker := filepath.Join(t.TempDir(), "opened-tty")
	sshScript(t, repo, "if exec 3<>/dev/tty; then echo yes > "+marker+"; read answer <&3; fi\necho 'cannot prompt' >&2\nexit 255\n")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := time.Now()
	err := c.Fetch(ctx, validate(t, c, repo), git.FetchOptions{})
	if err == nil {
		t.Fatal("fetch must fail")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("fetch waited for a prompt")
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("the child process could open /dev/tty")
	}
	if strings.TrimSpace(git.Stderr(err)) == "" {
		t.Fatal("stderr must be captured")
	}
}

func TestTimeoutTerminatesProcessGroup(t *testing.T) {
	tu.IsolateGit(t)
	c := git.New("git")
	repo := tu.InitRepo(t, filepath.Join(tu.Canonical(t, t.TempDir()), "repo"))
	pidFile := filepath.Join(t.TempDir(), "pid")
	sshScript(t, repo, "sleep 60 &\necho $! > "+pidFile+"\nwait\n")

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := c.Fetch(ctx, validate(t, c, repo), git.FetchOptions{}); err == nil {
		t.Fatal("fetch must fail on timeout")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("timeout was not enforced")
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	deadline := time.Now().Add(5 * time.Second)
	for {
		if !running(pid) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("grandchild process %d survived the timeout", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// running reports whether pid is a live process. Zombies count as terminated:
// in containers whose PID 1 does not reap orphans, such as GitHub Actions job
// containers, a killed grandchild stays a zombie indefinitely.
func running(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return true // no procfs, for example on macOS
	}
	// The state follows the command name, which is enclosed in parentheses.
	if i := strings.LastIndexByte(string(stat), ')'); i >= 0 && i+2 < len(stat) {
		return stat[i+2] != 'Z'
	}
	return true
}
