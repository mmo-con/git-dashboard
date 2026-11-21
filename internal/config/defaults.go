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
)

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
		Lazygit: Lazygit{Command: DefaultLazygitCommand, Enabled: true},
	}
}
