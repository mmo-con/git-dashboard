package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeGit writes a shell script that stands in for the Git executable.
func fakeGit(t *testing.T, body string) (runner, string) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	exe := filepath.Join(dir, "git")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\nOUT="+out+"\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return runner{executable: exe}, out
}

func TestCancelTerminatesBeforeKilling(t *testing.T) {
	r, out := fakeGit(t, "trap 'echo term > $OUT; exit 143' TERM\necho started > $OUT\nwhile :; do sleep 0.05; done\n")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			if data, _ := os.ReadFile(out); strings.Contains(string(data), "started") {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	start := time.Now()
	_, err := r.run(ctx, "", false, "fetch")
	if err == nil {
		t.Fatal("cancelled command must fail")
	}
	if d := time.Since(start); d >= terminationGrace {
		t.Fatalf("SIGTERM was not honored; took %v", d)
	}
	if data, _ := os.ReadFile(out); strings.TrimSpace(string(data)) != "term" {
		t.Fatalf("process did not receive SIGTERM: %q", data)
	}
}

func TestCancelKillsAfterGrace(t *testing.T) {
	old := terminationGrace
	terminationGrace = 200 * time.Millisecond
	t.Cleanup(func() { terminationGrace = old })
	r, _ := fakeGit(t, "trap '' TERM\nwhile :; do sleep 0.05; done\n")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := r.run(ctx, "", false, "fetch"); err == nil {
		t.Fatal("cancelled command must fail")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("process ignoring SIGTERM was not killed; took %v", d)
	}
}

func TestGrandchildHoldingPipesDoesNotBlock(t *testing.T) {
	old := pipeDelay
	pipeDelay = 100 * time.Millisecond
	t.Cleanup(func() { pipeDelay = old })
	// The background sleep inherits stdout and stderr, like a persistent ssh
	// connection master.
	r, _ := fakeGit(t, "sleep 5 &\necho ok\nexit 0\n")
	start := time.Now()
	res, err := r.run(context.Background(), "", false, "fetch")
	if err != nil {
		t.Fatalf("successful command reported %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Wait blocked on the grandchild for %v", d)
	}
	if strings.TrimSpace(string(res.stdout)) != "ok" {
		t.Fatalf("stdout = %q", res.stdout)
	}
}
