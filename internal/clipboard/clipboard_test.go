package clipboard

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func names(tools []Tool) string {
	var out []string
	for _, t := range tools {
		out = append(out, strings.Join(append([]string{t.Name}, t.Args...), " "))
	}
	return strings.Join(out, ", ")
}

func TestCandidates(t *testing.T) {
	cases := []struct {
		goos string
		env  map[string]string
		want string
	}{
		{"darwin", nil, "pbcopy"},
		{"linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"},
			"wl-copy, xclip -selection clipboard, xsel --clipboard --input"},
		{"linux", map[string]string{"DISPLAY": ":0"}, "xclip -selection clipboard, xsel --clipboard --input"},
		{"linux", nil, ""},
	}
	for _, tc := range cases {
		if got := names(Candidates(tc.goos, env(tc.env))); got != tc.want {
			t.Errorf("%s %v: got %q, want %q", tc.goos, tc.env, got, tc.want)
		}
	}
}

// tool writes an executable shell script, like the controlled helper of
// docs/adr/0008-compatibility-performance-and-testing.md.
func tool(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCopyPassesArgumentsAndText(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	exe := tool(t, "fake-copy", `echo "$@" > `+out+`.args; cat > `+out)
	used, err := Clipboard{Tools: []Tool{{Name: exe, Args: []string{"-selection", "clipboard"}}}}.
		Copy(context.Background(), "/r/my repo")
	if err != nil || used != "fake-copy" {
		t.Fatalf("used %q, err %v", used, err)
	}
	args, _ := os.ReadFile(out + ".args")
	text, _ := os.ReadFile(out)
	if string(args) != "-selection clipboard\n" || string(text) != "/r/my repo" {
		t.Fatalf("args %q, text %q", args, text)
	}
}

func TestCopyFallsBackToTheNextTool(t *testing.T) {
	broken := tool(t, "broken", `echo "Error: Can't open display" >&2; exit 1`)
	works := tool(t, "works", `cat > /dev/null`)
	used, err := Clipboard{Tools: []Tool{{Name: broken}, {Name: works}}}.Copy(context.Background(), "x")
	if err != nil || used != "works" {
		t.Fatalf("used %q, err %v", used, err)
	}

	_, err = Clipboard{Tools: []Tool{{Name: works + "-missing"}, {Name: broken}}}.Copy(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "broken") || !strings.Contains(err.Error(), "Can't open display") {
		t.Fatalf("err = %v", err)
	}
}

func TestCopyWithoutTools(t *testing.T) {
	_, err := Clipboard{Tools: []Tool{{Name: "no-such-copy-xyz"}, {Name: "no-such-sel-xyz"}}}.Copy(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "no-such-copy-xyz, no-such-sel-xyz") {
		t.Fatalf("err = %v", err)
	}
	_, err = Clipboard{}.Copy(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "DISPLAY") {
		t.Fatalf("err without candidates = %v", err)
	}
}

func TestCopyTimesOut(t *testing.T) {
	hang := tool(t, "hang", `sleep 10`)
	start := time.Now()
	_, err := Clipboard{Tools: []Tool{{Name: hang}}, Timeout: 200 * time.Millisecond}.Copy(context.Background(), "x")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("copy took %v", d)
	}
}

// TestCopyReturnsWhileToolServesSelection checks a tool that, like wl-copy or
// xclip, leaves a background process holding its inherited output.
func TestCopyReturnsWhileToolServesSelection(t *testing.T) {
	serve := tool(t, "serve", `cat > /dev/null; sleep 5 &`)
	start := time.Now()
	if _, err := (Clipboard{Tools: []Tool{{Name: serve}}}).Copy(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("copy waited %v for the background process", d)
	}
}

func TestCheckRejectsUnsafeText(t *testing.T) {
	for _, s := range []string{"", "a\nb", "a\rb", "a\x1b[31mb", "a\x00b", "a\x7fb", "a\xffb"} {
		if Check(s) == nil {
			t.Errorf("%q must be rejected", s)
		}
	}
	if err := Check("/home/me/Projekte/größe café"); err != nil {
		t.Error(err)
	}
	works := tool(t, "works", `cat > /dev/null`)
	if _, err := (Clipboard{Tools: []Tool{{Name: works}}}).Copy(context.Background(), "a\nb"); err == nil {
		t.Error("Copy must check the text")
	}
}
