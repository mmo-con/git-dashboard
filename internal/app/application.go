// Package app contains the application services used by the TUI. It
// coordinates configuration, inventory, the Git client, the scheduler, and the
// launchers without depending on the TUI.
package app

import (
	"context"
	"os"
	"sync/atomic"
	"time"

	"github.com/mmo-con/headquarters/internal/config"
	"github.com/mmo-con/headquarters/internal/git"
	"github.com/mmo-con/headquarters/internal/inventory"
	"github.com/mmo-con/headquarters/internal/jobs"
	"github.com/mmo-con/headquarters/internal/launcher"
)

// Application bundles the services of one headquarters session.
type Application struct {
	ConfigPath string
	// ScanRoot, if set, replaces the configured inventory with discovery below
	// this directory, also after a reload.
	ScanRoot  string
	Config    *config.Config
	Git       *git.Client
	Scheduler *jobs.Scheduler
	Lazygit   *launcher.Lazygit
	Launchers []launcher.Custom

	generation atomic.Uint64
}

// New creates the application services for a loaded configuration.
func New(configPath string, cfg *config.Config, client *git.Client) *Application {
	s := jobs.NewScheduler(limits(cfg))
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

func limits(cfg *config.Config) map[jobs.Kind]int {
	return map[jobs.Kind]int{
		jobs.KindInspect: cfg.Git.InspectionParallelism,
		jobs.KindFetch:   cfg.Git.FetchParallelism,
		jobs.KindUpdate:  cfg.Git.FetchParallelism,
	}
}

// SetScanRoot replaces the configured inventory with discovery below root.
// The replacement is applied again whenever the configuration is reloaded.
func (a *Application) SetScanRoot(root string) {
	a.ScanRoot = root
	a.Config.ScanOnly(root)
}

// LoadInventory resolves the inventory of the current configuration.
func (a *Application) LoadInventory(ctx context.Context) inventory.Result {
	return BuildInventory(ctx, a.Config, a.Git)
}

// BuildInventory resolves the inventory of cfg. It does not access the
// Application and may therefore run concurrently with the user interface.
func BuildInventory(ctx context.Context, cfg *config.Config, client *git.Client) inventory.Result {
	res := inventory.Build(ctx, cfg, client)
	res.Warnings = append(append([]string{}, cfg.Warnings...), res.Warnings...)
	return res
}

// Loaded is a configuration that has been read and checked but is not active
// yet.
type Loaded struct {
	Config *config.Config
	Git    *git.Client
}

// LoadConfig reads the configuration file again and checks its Git
// executable. It does not change the Application, so it may run concurrently
// with the user interface; Activate makes the result effective.
func (a *Application) LoadConfig(ctx context.Context) (*Loaded, error) {
	cfg, err := config.Load(a.ConfigPath, os.Getenv)
	if err != nil {
		return nil, err
	}
	if a.ScanRoot != "" {
		cfg.ScanOnly(a.ScanRoot)
	}
	client := git.New(cfg.Git.Executable)
	vctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if _, err := client.CheckVersion(vctx); err != nil {
		return nil, err
	}
	return &Loaded{Config: cfg, Git: client}, nil
}

// Activate makes a loaded configuration effective: the Git executable,
// parallelism limits, lazygit, and launchers. It must be called from the
// goroutine that owns the Application, such as the Bubble Tea update loop.
func (a *Application) Activate(l *Loaded) {
	a.Config = l.Config
	a.Git = l.Git
	a.Scheduler.SetLimits(limits(l.Config))
	a.Lazygit = launcher.NewLazygit(l.Config.Lazygit, l.Config.File)
	a.Launchers = launcher.NewCustom(l.Config.Launchers)
}

// NextGeneration returns a new refresh generation.
func (a *Application) NextGeneration() uint64 { return a.generation.Add(1) }

// FetchOptions returns the configured fetch options.
func (a *Application) FetchOptions() git.FetchOptions {
	return git.FetchOptions{Prune: a.Config.Git.FetchPrune}
}

// Close cancels all running work.
func (a *Application) Close() { a.Scheduler.CancelAll() }
