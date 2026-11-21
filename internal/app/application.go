// Package app contains the application services used by the TUI. It
// coordinates configuration, inventory, the Git client, the scheduler, and the
// launchers without depending on the TUI.
package app

import (
	"context"
	"os"
	"sync/atomic"

	"github.com/mmo-con/git-dashboard/internal/config"
	"github.com/mmo-con/git-dashboard/internal/git"
	"github.com/mmo-con/git-dashboard/internal/inventory"
	"github.com/mmo-con/git-dashboard/internal/jobs"
	"github.com/mmo-con/git-dashboard/internal/launcher"
)

// Application bundles the services of one Git Dashboard session.
type Application struct {
	ConfigPath string
	Config     *config.Config
	Git        *git.Client
	Scheduler  *jobs.Scheduler
	Lazygit    *launcher.Lazygit
	Launchers  []launcher.Custom

	generation atomic.Uint64
}

// New creates the application services for a loaded configuration.
func New(configPath string, cfg *config.Config, client *git.Client) *Application {
	s := jobs.NewScheduler(map[jobs.Kind]int{
		jobs.KindInspect: cfg.Git.InspectionParallelism,
		jobs.KindFetch:   cfg.Git.FetchParallelism,
		jobs.KindUpdate:  cfg.Git.FetchParallelism,
	})
	s.Detail = func(err error) (*int, string) { return git.ExitCode(err), git.Stderr(err) }
	return &Application{
		ConfigPath: configPath,
		Config:     cfg,
		Git:        client,
		Scheduler:  s,
		Lazygit:    launcher.NewLazygit(cfg.Lazygit, cfg.File),
		Launchers:  launcher.NewCustom(cfg.Launchers),
	}
}

// LoadInventory resolves the inventory of the current configuration.
func (a *Application) LoadInventory(ctx context.Context) inventory.Result {
	res := inventory.Build(ctx, a.Config, a.Git)
	res.Warnings = append(append([]string{}, a.Config.Warnings...), res.Warnings...)
	return res
}

// Reload reads the configuration file again. On error the current
// configuration stays active.
func (a *Application) Reload() error {
	cfg, err := config.Load(a.ConfigPath, os.Getenv)
	if err != nil {
		return err
	}
	a.Config = cfg
	a.Lazygit = launcher.NewLazygit(cfg.Lazygit, cfg.File)
	a.Launchers = launcher.NewCustom(cfg.Launchers)
	return nil
}

// NextGeneration returns a new refresh generation.
func (a *Application) NextGeneration() uint64 { return a.generation.Add(1) }

// FetchOptions returns the configured fetch options.
func (a *Application) FetchOptions() git.FetchOptions {
	return git.FetchOptions{Prune: a.Config.Git.FetchPrune}
}

// Close cancels all running work.
func (a *Application) Close() { a.Scheduler.CancelAll() }
