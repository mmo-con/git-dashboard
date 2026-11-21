package app_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mmo-con/git-dashboard/internal/app"
	"github.com/mmo-con/git-dashboard/internal/config"
	"github.com/mmo-con/git-dashboard/internal/git"
	tu "github.com/mmo-con/git-dashboard/internal/testutil"
)

// TestInspectionPerformance verifies the target from the design: 200
// repositories are inspected within 2 seconds. Repository creation is not
// measured. Enable with GIT_DASHBOARD_PERF=1.
func TestInspectionPerformance(t *testing.T) {
	if os.Getenv("GIT_DASHBOARD_PERF") == "" {
		t.Skip("set GIT_DASHBOARD_PERF=1 to run the performance test")
	}
	tu.IsolateGit(t)
	base := tu.Canonical(t, t.TempDir())
	template := tu.InitRepo(t, filepath.Join(base, "template"))
	for i := 0; i < 50; i++ {
		tu.WriteFile(t, filepath.Join(template, fmt.Sprintf("dir%d/file%d.txt", i%5, i)), "content\n")
	}
	tu.Git(t, template, "add", ".")
	tu.Git(t, template, "commit", "-q", "-m", "files")
	root := filepath.Join(base, "repos")
	for i := 0; i < 200; i++ {
		tu.Git(t, "", "clone", "-q", template, filepath.Join(root, fmt.Sprintf("group%d/repo%03d", i%10, i)))
	}

	cfg := config.Default()
	cfg.Discovery = []config.Discovery{{Root: root, MaxDepth: 2, Include: []string{"**"}}}
	a := app.New("", cfg, git.New("git"))
	defer a.Close()

	start := time.Now()
	res := a.LoadInventory(context.Background())
	if len(res.Repositories) != 200 {
		t.Fatalf("found %d repositories", len(res.Repositories))
	}
	inventoryTime := time.Since(start)
	js := app.InspectJobs(a.Git, res.Repositories, 1, cfg.Git.InspectionTimeout)
	a.Scheduler.Submit(js)
	done := 0
	for done < len(js) {
		for _, e := range a.Scheduler.Events.Next() {
			if e.Result.Status.Final() {
				p := e.Payload.(app.InspectionPayload)
				if p.Err != nil {
					t.Fatal(p.Err)
				}
				done++
			}
		}
	}
	total := time.Since(start)
	t.Logf("inventory %v, total %v", inventoryTime, total)
	if total > 2*time.Second {
		t.Fatalf("inspection of 200 repositories took %v, target is 2s", total)
	}
}
