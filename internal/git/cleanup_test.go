package git_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mmo-con/headquarters/internal/git"
	tu "github.com/mmo-con/headquarters/internal/testutil"
)

// mergedBranchFixture returns a clone whose branch done is merged into
// origin/main on the server, with its upstream deleted, while local main is
// still behind; wip has a commit of its own and side is checked out in a
// linked worktree.
func mergedBranchFixture(t *testing.T) (clone, worktree string) {
	t.Helper()
	tu.IsolateGit(t)
	dir := tu.Canonical(t, t.TempDir())
	up := filepath.Join(dir, "up.git")
	tu.Git(t, "", "init", "-q", "--bare", up)
	clone = filepath.Join(dir, "clone")
	tu.Git(t, "", "clone", "-q", up, clone)
	tu.Commit(t, clone, "a", "a\n", "init")
	tu.Git(t, clone, "push", "-q", "origin", "main")
	tu.Git(t, clone, "remote", "set-head", "origin", "--auto")
	tu.Git(t, clone, "switch", "-q", "-c", "done")
	tu.Commit(t, clone, "b", "b\n", "done work")
	tu.Git(t, clone, "push", "-q", "-u", "origin", "done")
	tu.Git(t, clone, "switch", "-q", "-c", "wip", "main")
	tu.Commit(t, clone, "c", "c\n", "wip work")
	tu.Git(t, clone, "switch", "-q", "main")
	worktree = filepath.Join(dir, "wt")
	tu.Git(t, clone, "worktree", "add", "-q", "-b", "side", worktree)

	other := filepath.Join(dir, "other")
	tu.Git(t, "", "clone", "-q", up, other)
	tu.Git(t, other, "merge", "-q", "--no-ff", "-m", "merge done", "origin/done")
	tu.Git(t, other, "push", "-q", "origin", "main", ":done")
	tu.Git(t, clone, "fetch", "-q", "--prune")
	return clone, worktree
}

func TestReadBranchCleanup(t *testing.T) {
	clone, worktree := mergedBranchFixture(t)
	c := git.New("git")
	loc := validate(t, c, clone)

	st, err := c.ReadBranchCleanup(context.Background(), loc, "origin")
	if err != nil {
		t.Fatal(err)
	}
	if st.Remote != "origin" || st.Default != "refs/remotes/origin/main" ||
		st.DefaultID != tu.Git(t, clone, "rev-parse", "origin/main") {
		t.Fatalf("default = %q %q %q", st.Remote, st.Default, st.DefaultID)
	}
	got := map[string]git.CleanupBranch{}
	for _, b := range st.Branches {
		got[b.Name] = b
	}
	if len(got) != 4 {
		t.Fatalf("branches = %+v", st.Branches)
	}
	done := got["done"]
	if done.CommitID != tu.Git(t, clone, "rev-parse", "done") || done.Upstream != "origin/done" || !done.Gone ||
		!done.MergedDefault || done.MergedHead || done.Worktree != "" {
		t.Fatalf("done = %+v", done)
	}
	if wip := got["wip"]; wip.MergedDefault || wip.Upstream != "" || wip.Gone {
		t.Fatalf("wip = %+v", wip)
	}
	if side := got["side"]; side.Worktree != worktree || !side.MergedDefault || !side.MergedHead {
		t.Fatalf("side = %+v", side)
	}
	if main := got["main"]; main.Worktree != clone || main.Upstream != "origin/main" {
		t.Fatalf("main = %+v", main)
	}

	tu.Git(t, clone, "merge", "-q", "--ff-only", "origin/main")
	st, err = c.ReadBranchCleanup(context.Background(), loc, "origin")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range st.Branches {
		if b.Name == "done" && !b.MergedHead {
			t.Fatalf("done must be merged into HEAD after the update: %+v", b)
		}
	}
}

func TestReadBranchCleanupWithoutDefaultBranch(t *testing.T) {
	clone, _ := mergedBranchFixture(t)
	c := git.New("git")
	loc := validate(t, c, clone)
	tu.Git(t, clone, "remote", "set-head", "origin", "--delete")

	for _, remote := range []string{"origin", ""} {
		st, err := c.ReadBranchCleanup(context.Background(), loc, remote)
		if err != nil {
			t.Fatalf("remote %q: %v", remote, err)
		}
		if st.Default != "" || st.Remote != remote {
			t.Fatalf("remote %q: default = %q", remote, st.Default)
		}
		for _, b := range st.Branches {
			if b.MergedDefault {
				t.Fatalf("remote %q: nothing is merged into an unknown default: %+v", remote, b)
			}
		}
	}
}

func TestDeleteBranch(t *testing.T) {
	clone, _ := mergedBranchFixture(t)
	c := git.New("git")
	loc := validate(t, c, clone)
	ctx := context.Background()

	if err := c.DeleteBranch(ctx, loc, "done"); err == nil || !strings.Contains(err.Error(), "not fully merged") {
		t.Fatalf("git branch -d must refuse a branch not merged into HEAD: %v", err)
	}
	tu.Git(t, clone, "merge", "-q", "--ff-only", "origin/main")
	if err := c.DeleteBranch(ctx, loc, "done"); err != nil {
		t.Fatal(err)
	}
	if out, _ := tu.TryGit(clone, "rev-parse", "--verify", "-q", "refs/heads/done"); out != "" {
		t.Fatalf("done still exists: %s", out)
	}
	if err := c.DeleteBranch(ctx, loc, "wip"); err == nil {
		t.Fatal("an unmerged branch must not be deleted")
	}
	if err := c.DeleteBranch(ctx, loc, "-D"); err == nil {
		t.Fatal("a name that looks like an option must be rejected")
	}
}

// serverMergedFixture returns a clone with branches whose upstreams were
// deleted after the server merged them in different ways:
//
//   - rebased: rebased onto main and fast-forwarded, so its commits have new IDs;
//   - squashed: squash-merged;
//   - reworked: squash-merged, then the same line was changed again on main;
//   - spaced: applied on main with different whitespace;
//   - dropped: deleted on the server without a merge.
func serverMergedFixture(t *testing.T) string {
	t.Helper()
	tu.IsolateGit(t)
	dir := tu.Canonical(t, t.TempDir())
	up := filepath.Join(dir, "up.git")
	tu.Git(t, "", "init", "-q", "--bare", up)
	clone := filepath.Join(dir, "clone")
	tu.Git(t, "", "clone", "-q", up, clone)
	tu.Commit(t, clone, "base", "base\n", "init")
	tu.Git(t, clone, "push", "-q", "origin", "main")
	tu.Git(t, clone, "remote", "set-head", "origin", "--auto")
	branch := func(name string, files ...string) {
		tu.Git(t, clone, "switch", "-q", "-c", name, "main")
		for _, f := range files {
			tu.Commit(t, clone, f, name+" "+f+"\n", name+" "+f)
		}
		tu.Git(t, clone, "push", "-q", "-u", "origin", name)
	}
	branch("rebased", "r1", "r2")
	branch("squashed", "s1", "s2")
	branch("reworked", "w1")
	tu.Git(t, clone, "switch", "-q", "-c", "spaced", "main")
	tu.Commit(t, clone, "code", "x = 1\n", "spaced")
	tu.Git(t, clone, "push", "-q", "-u", "origin", "spaced")
	branch("dropped", "d1")
	tu.Git(t, clone, "switch", "-q", "main")

	srv := filepath.Join(dir, "server")
	tu.Git(t, "", "clone", "-q", up, srv)
	tu.Commit(t, srv, "other", "other\n", "unrelated work on main")
	tu.Git(t, srv, "switch", "-q", "-c", "tmp", "origin/rebased")
	tu.Git(t, srv, "rebase", "-q", "main")
	tu.Git(t, srv, "switch", "-q", "main")
	tu.Git(t, srv, "merge", "-q", "--ff-only", "tmp")
	for _, b := range []string{"squashed", "reworked"} {
		tu.Git(t, srv, "merge", "-q", "--squash", "origin/"+b)
		tu.Git(t, srv, "commit", "-q", "-m", "squash "+b)
	}
	tu.Commit(t, srv, "w1", "reworked again\n", "rework w1")
	tu.Commit(t, srv, "code", "x  =  1\n", "spaced differently")
	tu.Git(t, srv, "push", "-q", "origin", "main", ":rebased", ":squashed", ":reworked", ":spaced", ":dropped")
	tu.Git(t, clone, "fetch", "-q", "--prune")
	return clone
}

func TestReadBranchCleanupDetectsSquashAndRebaseMerges(t *testing.T) {
	clone := serverMergedFixture(t)
	c, v, err := git.Open(context.Background(), "git")
	if err != nil {
		t.Fatal(err)
	}
	if !v.AtLeast(git.Version{Major: 2, Minor: 38}) {
		t.Skipf("git %s has no merge-tree --write-tree", v.Raw)
	}
	st, err := c.ReadBranchCleanup(context.Background(), validate(t, c, clone), "origin")
	if err != nil {
		t.Fatal(err)
	}
	if !st.ContainmentChecked {
		t.Fatal("containment must be checked with Git 2.38 or newer")
	}
	want := map[string]bool{"rebased": true, "squashed": true, "reworked": false, "spaced": false, "dropped": false, "main": false}
	for _, b := range st.Branches {
		if b.MergedDefault && b.Name != "main" {
			t.Fatalf("%s must not be merged by ancestry: %+v", b.Name, b)
		}
		if b.Contained != want[b.Name] {
			t.Errorf("%s: contained = %v, want %v", b.Name, b.Contained, want[b.Name])
		}
	}
}

func TestReadBranchCleanupWithoutMergeTree(t *testing.T) {
	clone := serverMergedFixture(t)
	c := git.New("git") // assumes the minimum version, Git 2.34
	st, err := c.ReadBranchCleanup(context.Background(), validate(t, c, clone), "origin")
	if err != nil {
		t.Fatal(err)
	}
	if st.ContainmentChecked {
		t.Fatal("containment needs Git 2.38")
	}
	for _, b := range st.Branches {
		if b.Contained {
			t.Fatalf("%s must not be contained without a check", b.Name)
		}
	}
}

func TestDeleteBranchRefChecksTheCommit(t *testing.T) {
	clone := serverMergedFixture(t)
	c := git.New("git")
	loc := validate(t, c, clone)
	ctx := context.Background()
	tip := tu.Git(t, clone, "rev-parse", "squashed")
	if err := c.DeleteBranchRef(ctx, loc, "squashed", tu.Git(t, clone, "rev-parse", "main")); err == nil {
		t.Fatal("a branch that moved must not be deleted")
	}
	tu.Git(t, clone, "rev-parse", "--verify", "-q", "refs/heads/squashed")
	if err := c.DeleteBranchRef(ctx, loc, "squashed", tip); err != nil {
		t.Fatal(err)
	}
	if out, _ := tu.TryGit(clone, "rev-parse", "--verify", "-q", "refs/heads/squashed"); out != "" {
		t.Fatal("squashed must be deleted")
	}
	if err := c.DeleteBranchRef(ctx, loc, "-d", tip); err == nil {
		t.Fatal("a name that looks like an option must be rejected")
	}
	if err := c.DeleteBranchRef(ctx, loc, "rebased", "HEAD"); err == nil {
		t.Fatal("only a full commit ID is accepted")
	}
}
