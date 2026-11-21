package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseStatusHeadersAndCounts(t *testing.T) {
	out := "# branch.oid 1234567890abcdef\x00" +
		"# branch.head main\x00" +
		"# branch.upstream origin/main\x00" +
		"# branch.ab +2 -3\x00" +
		"# stash 4\x00" + // unknown header: ignored
		"1 M. N... 100644 100644 100644 aaa bbb staged.txt\x00" +
		"1 .M N... 100644 100644 100644 aaa bbb modified.txt\x00" +
		"1 MM N... 100644 100644 100644 aaa bbb both.txt\x00" +
		"1 .D N... 100644 100644 000000 aaa aaa deleted.txt\x00" +
		"1 D. N... 100644 000000 000000 aaa 000 staged-delete.txt\x00" +
		"1 .T N... 100644 100644 120000 aaa aaa typechange\x00" +
		"2 R. N... 100644 100644 100644 aaa aaa R100 new name.txt\x00old name.txt\x00" +
		"u UU N... 100644 100644 100644 100644 aaa bbb ccc conflict.txt\x00" +
		"? untracked.txt\x00" +
		"! ignored.txt\x00"
	st, err := ParseStatus([]byte(out), true)
	if err != nil {
		t.Fatal(err)
	}
	if st.Head.Branch != "main" || st.Head.CommitID != "1234567890abcdef" || st.Head.Detached || st.Head.Unborn {
		t.Errorf("head = %+v", st.Head)
	}
	u := st.Upstream
	if !u.Configured || u.Gone || u.Ref != "origin/main" || u.Ahead != 2 || u.Behind != 3 || !u.Diverged() {
		t.Errorf("upstream = %+v", u)
	}
	w := st.Worktree
	want := struct{ staged, modified, deleted, renamed, conflicts, untracked int }{4, 3, 1, 1, 1, 1}
	got := struct{ staged, modified, deleted, renamed, conflicts, untracked int }{w.Staged, w.Modified, w.Deleted, w.Renamed, w.Conflicts, w.Untracked}
	if got != want {
		t.Errorf("counts = %+v, want %+v", got, want)
	}
	if !w.UntrackedChecked || w.Clean() {
		t.Errorf("worktree = %+v", w)
	}
}

func TestParseStatusSpecialHeads(t *testing.T) {
	tests := []struct {
		name  string
		out   string
		check func(t *testing.T, st Status)
	}{
		{"unborn", "# branch.oid (initial)\x00# branch.head main\x00", func(t *testing.T, st Status) {
			if !st.Head.Unborn || st.Head.Branch != "main" || st.Upstream.Configured {
				t.Errorf("%+v", st)
			}
		}},
		{"detached", "# branch.oid abc\x00# branch.head (detached)\x00", func(t *testing.T, st Status) {
			if !st.Head.Detached || st.Head.Branch != "" {
				t.Errorf("%+v", st)
			}
		}},
		{"upstream gone", "# branch.oid abc\x00# branch.head main\x00# branch.upstream origin/main\x00", func(t *testing.T, st Status) {
			if !st.Upstream.Configured || !st.Upstream.Gone {
				t.Errorf("%+v", st.Upstream)
			}
		}},
		{"clean", "# branch.oid abc\x00# branch.head main\x00", func(t *testing.T, st Status) {
			if !st.Worktree.Clean() {
				t.Errorf("%+v", st.Worktree)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, err := ParseStatus([]byte(tc.out), true)
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, st)
		})
	}
}

func TestParseStatusRejectsUnknownEntryType(t *testing.T) {
	if _, err := ParseStatus([]byte("# branch.oid abc\x00X something\x00"), true); err == nil {
		t.Fatal("expected a compatibility error")
	}
}

func TestParseStatusFixture(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "git-output", "status-merge-conflict.bin"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := ParseStatus(data, true)
	if err != nil {
		t.Fatal(err)
	}
	if st.Worktree.Conflicts != 1 || st.Worktree.Untracked != 1 || st.Head.Branch != "main" {
		t.Errorf("%+v", st)
	}
}

func TestParseVersion(t *testing.T) {
	tests := map[string]Version{
		"git version 2.34.1\n":               {Major: 2, Minor: 34, Patch: 1},
		"git version 2.39.2 (Apple Git-143)": {Major: 2, Minor: 39, Patch: 2},
		"git version 2.45.0.windows.1":       {Major: 2, Minor: 45},
		"git version 2.50.0-rc1":             {Major: 2, Minor: 50},
		"git version 2.33.8.gabcdef.dirty\n": {Major: 2, Minor: 33, Patch: 8},
	}
	for in, want := range tests {
		got, err := ParseVersion(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got.Major != want.Major || got.Minor != want.Minor || got.Patch != want.Patch {
			t.Errorf("%q: got %v, want %v", in, got, want)
		}
	}
	if v, _ := ParseVersion("git version 2.33.8"); v.AtLeast(MinimumVersion) {
		t.Error("2.33.8 must be rejected")
	}
	if v, _ := ParseVersion("git version 2.34.0"); !v.AtLeast(MinimumVersion) {
		t.Error("2.34.0 must be accepted")
	}
	if _, err := ParseVersion("hello"); err == nil {
		t.Error("expected error")
	}
}

func TestParseRemotes(t *testing.T) {
	out := "remote.origin.url\ngit@github.com:example/api.git\x00" +
		"remote.my.fork.url\nhttps://example.com/fork.git\x00" +
		"remote.origin.url\nsecond-url\x00"
	got := parseRemotes([]byte(out))
	if len(got) != 2 || got[0].Name != "origin" || got[0].URL != "git@github.com:example/api.git" ||
		got[1].Name != "my.fork" || got[1].URL != "https://example.com/fork.git" {
		t.Errorf("%+v", got)
	}
}

func TestEnvironmentSanitization(t *testing.T) {
	env := sanitize([]string{"GIT_DIR=/x", "GIT_WORK_TREE=/y", "GIT_INDEX_FILE=i", "GIT_COMMON_DIR=c",
		"GIT_OBJECT_DIRECTORY=o", "GIT_NAMESPACE=n", "GIT_DIRECTORY=keep", "SSH_AUTH_SOCK=s", "HOME=/h"})
	want := []string{"GIT_DIRECTORY=keep", "SSH_AUTH_SOCK=s", "HOME=/h"}
	if len(env) != len(want) {
		t.Fatalf("got %v", env)
	}
	for i := range want {
		if env[i] != want[i] {
			t.Fatalf("got %v", env)
		}
	}

	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	t.Setenv("GIT_OPTIONAL_LOCKS", "1")
	bg := envMap(backgroundEnvironment(true))
	for k, v := range map[string]string{"GIT_TERMINAL_PROMPT": "0", "GIT_OPTIONAL_LOCKS": "0", "GIT_PAGER": "cat", "GIT_EDITOR": "true"} {
		if bg[k] != v {
			t.Errorf("%s = %q, want %q", k, bg[k], v)
		}
	}
	if envMap(backgroundEnvironment(false))["GIT_OPTIONAL_LOCKS"] != "1" {
		t.Error("mutating commands must not override GIT_OPTIONAL_LOCKS")
	}
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		for i := 0; i < len(kv); i++ {
			if kv[i] == '=' {
				m[kv[:i]] = kv[i+1:]
				break
			}
		}
	}
	return m
}
