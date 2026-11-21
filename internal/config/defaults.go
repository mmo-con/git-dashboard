package config

import "time"

const (
	CurrentVersion               = 1
	DefaultGitExecutable         = "git"
	DefaultInspectionParallelism = 8
	DefaultInspectionTimeout     = 10 * time.Second
	DefaultFetchParallelism      = 4
	DefaultFetchTimeout          = 120 * time.Second
	DefaultMaxDepth              = 3
	DefaultLazygitCommand        = "lazygit"
	DefaultUntracked             = "normal"
	DefaultBranchesMaxAge        = 30 * 24 * time.Hour
)

// DefaultPinnedBranches are evaluated in every repository, regardless of age
// and ignore patterns.
var DefaultPinnedBranches = []string{"main", "master"}

// Default returns the configuration used when no file exists.
func Default() *Config {
	return &Config{
		Version: CurrentVersion,
		Git: Git{
			Executable:            DefaultGitExecutable,
			InspectionParallelism: DefaultInspectionParallelism,
			InspectionTimeout:     DefaultInspectionTimeout,
			FetchParallelism:      DefaultFetchParallelism,
			FetchTimeout:          DefaultFetchTimeout,
			FetchPrune:            true,
			Untracked:             DefaultUntracked,
		},
		Branches: Branches{
			Enabled: true,
			Pinned:  append([]string(nil), DefaultPinnedBranches...),
			MaxAge:  DefaultBranchesMaxAge,
		},
		Lazygit: Lazygit{Command: DefaultLazygitCommand, Enabled: true},
		UI:      UI{Icons: "unicode", View: "list", Theme: DefaultTheme()},
	}
}

// DefaultTheme returns the built-in colors.
func DefaultTheme() Theme {
	return Theme{
		ActiveBorder:       "green",
		InactiveBorder:     "gray",
		Title:              "green",
		SelectedBackground: "blue",
		Key:                "cyan",
		Branch:             "cyan",
		Clean:              "gray",
		Dirty:              "yellow",
		Ahead:              "green",
		Behind:             "magenta",
		Error:              "red",
		Warning:            "yellow",
		Dim:                "gray",
	}
}
