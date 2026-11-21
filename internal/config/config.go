// Package config loads and validates the Git Dashboard configuration file.
package config

import (
	"fmt"
	"time"
)

// Config is the validated configuration with all paths expanded.
type Config struct {
	Version      int
	File         string // absolute path of the loaded file, empty if none exists
	Repositories []Repository
	Discovery    []Discovery
	Git          Git
	Lazygit      Lazygit
	Launchers    []Launcher
	Warnings     []string
}

type Repository struct {
	Path       string
	Family     string
	Enabled    bool
	Remote     string
	RemoteName string
	Untracked  string // empty means the [git] default
}

type Discovery struct {
	Root           string
	MaxDepth       int
	Family         string
	Include        []string
	Exclude        []string
	FollowSymlinks bool
}

type Git struct {
	Executable            string
	InspectionParallelism int
	InspectionTimeout     time.Duration
	FetchParallelism      int
	FetchTimeout          time.Duration
	FetchPrune            bool
	Untracked             string
}

type Lazygit struct {
	Command string
	Args    []string
	Enabled bool
}

type Launcher struct {
	Name    string
	Command []string
}

// Duration decodes Go duration strings such as "10s".
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", string(b), err)
	}
	if v <= 0 {
		return fmt.Errorf("duration %q must be positive", string(b))
	}
	d.Duration = v
	return nil
}

// file mirrors the TOML layout. Pointers distinguish absent values.
type file struct {
	Version      int              `toml:"version"`
	Repositories []fileRepository `toml:"repositories"`
	Discovery    []fileDiscovery  `toml:"discovery"`
	Git          fileGit          `toml:"git"`
	Lazygit      fileLazygit      `toml:"lazygit"`
	Launchers    []fileLauncher   `toml:"launchers"`
}

type fileRepository struct {
	Path       string `toml:"path"`
	Family     string `toml:"family"`
	Enabled    *bool  `toml:"enabled"`
	Remote     string `toml:"remote"`
	RemoteName string `toml:"remote_name"`
	Untracked  string `toml:"untracked"`
}

type fileDiscovery struct {
	Root           string   `toml:"root"`
	MaxDepth       *int     `toml:"max_depth"`
	Family         string   `toml:"family"`
	Include        []string `toml:"include"`
	Exclude        []string `toml:"exclude"`
	FollowSymlinks bool     `toml:"follow_symlinks"`
}

type fileGit struct {
	Executable            string    `toml:"executable"`
	InspectionParallelism *int      `toml:"inspection_parallelism"`
	InspectionTimeout     *Duration `toml:"inspection_timeout"`
	FetchParallelism      *int      `toml:"fetch_parallelism"`
	FetchTimeout          *Duration `toml:"fetch_timeout"`
	FetchPrune            *bool     `toml:"fetch_prune"`
	Untracked             string    `toml:"untracked"`
}

type fileLazygit struct {
	Command string   `toml:"command"`
	Args    []string `toml:"args"`
	Enabled *bool    `toml:"enabled"`
}

type fileLauncher struct {
	Name    string   `toml:"name"`
	Command []string `toml:"command"`
}
