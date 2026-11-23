package domain

import (
	"strings"
	"testing"
	"time"
)

func TestPrintable(t *testing.T) {
	for in, want := range map[string]string{
		"fix: ünïcode ✓ subject":            "fix: ünïcode ✓ subject",
		"evil \x1b]0;title\x07 \x1b[2J end": "evil \uFFFD]0;title\uFFFD \uFFFD[2J end",
		"c1 \u009b31m csi":                  "c1 \uFFFD31m csi",
		"raw \x9b byte":                     "raw \uFFFD byte",
		"tab\tand\nnewline\rdel\x7f":        "tab and newline\uFFFDdel\uFFFD",
	} {
		if got := Printable(in); got != want {
			t.Errorf("Printable(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRedactURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://user:token@github.com/o/r.git": "https://***@github.com/o/r.git",
		"HTTP://token@host/path@x":              "HTTP://***@host/path@x",
		"https://a@b@host/r":                    "https://***@host/r",
		"https://github.com/o/r.git":            "https://github.com/o/r.git",
		"git@github.com:o/r.git":                "git@github.com:o/r.git",
		"ssh://git@host/r.git":                  "ssh://git@host/r.git",
		"/local/path":                           "/local/path",
	} {
		if got := RedactURL(in); got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRemoteMismatch(t *testing.T) {
	remotes := KnownValue([]Remote{
		{Name: "upstream", URL: "git@host:u/r.git"},
		{Name: "origin", URL: " https://host/o/r/ "},
	})
	cases := []struct {
		name     string
		expected *ExpectedRemote
		remotes  Known[[]Remote]
		want     bool
	}{
		{"no assertion", nil, remotes, false},
		{"normalized match", &ExpectedRemote{URL: "https://host/o/r.git"}, remotes, false},
		{"other URL", &ExpectedRemote{URL: "https://host/x/r"}, remotes, true},
		{"named remote", &ExpectedRemote{Name: "upstream", URL: "git@host:u/r"}, remotes, false},
		{"remote missing", &ExpectedRemote{Name: "fork", URL: "git@host:u/r"}, remotes, true},
		{"state unknown", &ExpectedRemote{URL: "x"}, Known[[]Remote]{}, false},
	}
	for _, tc := range cases {
		r := Repository{ExpectedRemote: tc.expected, Inspection: Inspection{Remotes: tc.remotes}}
		if got := r.RemoteMismatch(); got != tc.want {
			t.Errorf("%s: got %v", tc.name, got)
		}
	}
}

func TestSelectPrimaryRemote(t *testing.T) {
	remotes := KnownValue([]Remote{{Name: "zeta"}, {Name: "origin"}, {Name: "alpha"}})
	cases := []struct {
		remotes    Known[[]Remote]
		configured string
		want       string
	}{
		{remotes, "zeta", "zeta"},
		{remotes, "missing", "origin"},
		{remotes, "", "origin"},
		{KnownValue([]Remote{{Name: "zeta"}, {Name: "alpha"}}), "", "alpha"},
	}
	for _, tc := range cases {
		if got := SelectPrimaryRemote(tc.remotes, tc.configured); got == nil || got.Name != tc.want {
			t.Errorf("configured %q: got %+v, want %s", tc.configured, got, tc.want)
		}
	}
	if SelectPrimaryRemote(KnownValue[[]Remote](nil), "") != nil || SelectPrimaryRemote(Known[[]Remote]{}, "") != nil {
		t.Error("no remotes must yield nil")
	}
}

func TestApplyInspectionUsesConfiguredPrimaryRemote(t *testing.T) {
	r := Repository{ExpectedRemote: &ExpectedRemote{Name: "fork"}}
	in := Inspection{Remotes: KnownValue([]Remote{{Name: "origin"}, {Name: "fork"}})}
	if !r.ApplyInspection(2, in, nil, time.Now()) || r.PrimaryRemote == nil || r.PrimaryRemote.Name != "fork" {
		t.Fatalf("primary remote = %+v", r.PrimaryRemote)
	}
	if r.ApplyInspection(1, Inspection{}, nil, time.Now()) || !r.Remotes.Valid {
		t.Fatal("an older generation must not overwrite newer state")
	}
}

func TestBranchPolicySelect(t *testing.T) {
	now := time.Unix(1_000_000_000, 0)
	day := 24 * time.Hour
	all := []BranchState{
		{Name: "feature/x", Current: true, CommitTime: now},
		{Name: "elsewhere", Worktree: "/other", CommitTime: now},
		{Name: "main", Upstream: "origin/main", Behind: 3, CommitTime: now.Add(-400 * day)},
		{Name: "develop", Upstream: "origin/develop", CommitTime: now},
		{Name: "wip", CommitTime: now.Add(-2 * day)},
		{Name: "pushed", Upstream: "origin/pushed", Ahead: 1, CommitTime: now.Add(-1 * day)},
		{Name: "old", Upstream: "origin/old", Gone: true, CommitTime: now.Add(-60 * day)},
		{Name: "archive/2020", CommitTime: now},
	}
	p := BranchPolicy{Enabled: true, Pinned: []string{"main", "release"}, Ignore: []string{"archive/**"}, MaxAge: 30 * day}

	var got []string
	for _, b := range p.Select(all, now) {
		got = append(got, b.Name)
	}
	// main is pinned despite its age; develop is up to date; old is too old;
	// archive/2020 is ignored; the missing pinned release is not reported.
	if strings.Join(got, " ") != "main pushed wip" {
		t.Fatalf("selected = %v", got)
	}

	p.ReportMissing = true
	sel := p.Select(all, now)
	if len(sel) != 4 || sel[1].Name != "release" || !sel[1].Missing || !sel[1].Pinned {
		t.Fatalf("missing pinned branch: %+v", sel)
	}

	p.Enabled = false
	if sel := p.Select(all, now); sel != nil {
		t.Fatalf("disabled policy selected %+v", sel)
	}
}
