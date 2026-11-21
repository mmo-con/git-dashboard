package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestParseExample(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "examples", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(data, "/etc/gd", env(map[string]string{"HOME": "/home/u"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repositories) != 4 || len(cfg.Discovery) != 2 || len(cfg.Launchers) != 2 {
		t.Fatalf("unexpected counts: %+v", cfg)
	}
	if cfg.Repositories[0].Path != "/home/u/devrepos/dotfiles" || !cfg.Repositories[0].Enabled {
		t.Errorf("repository 0: %+v", cfg.Repositories[0])
	}
	if cfg.Repositories[2].Enabled {
		t.Error("repository 2 must be disabled")
	}
	if cfg.Repositories[3].Untracked != "no" {
		t.Error("untracked override lost")
	}
	if cfg.Discovery[1].MaxDepth != 3 || cfg.Discovery[1].Include[0] != "**" {
		t.Errorf("discovery 1: %+v", cfg.Discovery[1])
	}
	if cfg.Git.FetchTimeout != 120*time.Second || cfg.Git.InspectionTimeout != 10*time.Second || !cfg.Git.FetchPrune {
		t.Errorf("git: %+v", cfg.Git)
	}
}

func TestDefaultsWhenEmpty(t *testing.T) {
	cfg, err := Parse([]byte("version = 1\n"), "/x", env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Git.Executable != "git" || cfg.Git.InspectionParallelism != 8 || cfg.Git.FetchParallelism != 4 ||
		!cfg.Git.FetchPrune || cfg.Lazygit.Command != "lazygit" || !cfg.Lazygit.Enabled || cfg.Git.Untracked != "normal" {
		t.Fatalf("%+v", cfg)
	}
}

func TestRejectsInvalidConfiguration(t *testing.T) {
	tests := map[string]string{
		"unknown field":      "version = 1\n[[repositories]]\npath = \"/a\"\nfamliy = \"x\"\n",
		"wrong type":         "version = 1\n[git]\nfetch_parallelism = \"four\"\n",
		"bad version":        "version = 2\n",
		"missing path":       "version = 1\n[[repositories]]\nfamily = \"x\"\n",
		"bad untracked":      "version = 1\n[git]\nuntracked = \"all\"\n",
		"bad duration":       "version = 1\n[git]\nfetch_timeout = \"soon\"\n",
		"bad parallelism":    "version = 1\n[git]\ninspection_parallelism = 0\n",
		"undefined variable": "version = 1\n[[repositories]]\npath = \"${NOPE}/x\"\n",
		"plain variable":     "version = 1\n[[repositories]]\npath = \"$HOME/x\"\n",
		"partial {path}":     "version = 1\n[[launchers]]\nname = \"x\"\ncommand = [\"x\", \"--dir={path}\"]\n",
		"empty launcher":     "version = 1\n[[launchers]]\nname = \"x\"\ncommand = []\n",
		"bad pattern":        "version = 1\n[[discovery]]\nroot = \"/r\"\nexclude = [\"[\"]\n",
		"bad depth":          "version = 1\n[[discovery]]\nroot = \"/r\"\nmax_depth = 0\n",
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(data), "/x", env(map[string]string{"HOME": "/h"})); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestUnknownFieldIsNamed(t *testing.T) {
	_, err := Parse([]byte("version = 1\n[git]\nexecutabel = \"git\"\n"), "/x", env(nil))
	if err == nil || !strings.Contains(err.Error(), "executabel") {
		t.Fatalf("error must name the field: %v", err)
	}
}

func TestPathExpansion(t *testing.T) {
	e := env(map[string]string{"HOME": "/home/u", "DEV": "/srv/dev"})
	tests := map[string]string{
		"~":                "/home/u",
		"~/repos":          "/home/u/repos",
		"${DEV}/api":       "/srv/dev/api",
		"${HOME}/a/../b":   "/home/u/b",
		"relative/repo":    "/cfg/relative/repo",
		"/abs/olute":       "/abs/olute",
		"~user-is-literal": "/cfg/~user-is-literal",
	}
	for in, want := range tests {
		got, err := expandPath(in, "/cfg", e)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	if got, _ := expandCommand("lazygit", "/cfg", e); got != "lazygit" {
		t.Errorf("bare command must stay a PATH lookup: %q", got)
	}
	if got, _ := expandCommand("~/bin/lazygit", "/cfg", e); got != "/home/u/bin/lazygit" {
		t.Errorf("command path: %q", got)
	}
}

func TestLocate(t *testing.T) {
	e := map[string]string{"HOME": "/home/u"}
	if p, _ := Locate("", env(e)); p != "/home/u/.config/git-dashboard/config.toml" {
		t.Errorf("default: %s", p)
	}
	e["XDG_CONFIG_HOME"] = "/xdg"
	if p, _ := Locate("", env(e)); p != "/xdg/git-dashboard/config.toml" {
		t.Errorf("xdg: %s", p)
	}
	e[EnvConfig] = "~/gd.toml"
	if p, _ := Locate("", env(e)); p != "/home/u/gd.toml" {
		t.Errorf("env: %s", p)
	}
	if p, _ := Locate("/flag.toml", env(e)); p != "/flag.toml" {
		t.Errorf("flag: %s", p)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "none.toml"), env(nil))
	if err != nil || cfg.File != "" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}
