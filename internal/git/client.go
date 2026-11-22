package git

import (
	"context"

	"github.com/mmo-con/git-dashboard/internal/domain"
)

// Client is the typed wrapper around the official Git CLI.
type Client struct {
	run runner
}

// New returns a client that runs the given Git executable.
func New(executable string) *Client {
	if executable == "" {
		executable = "git"
	}
	return &Client{run: runner{executable: executable}}
}

// Executable returns the configured Git executable.
func (c *Client) Executable() string { return c.run.executable }

type InspectOptions struct {
	Untracked domain.UntrackedMode
	// Branches reads all local branches instead of only the checked-out one.
	Branches bool
}

type FetchOptions struct {
	Prune bool
}

// Interface is implemented by Client and used by application services.
type Interface interface {
	Validate(ctx context.Context, path string) Validation
	Inspect(ctx context.Context, loc domain.Location, opts InspectOptions) (domain.Inspection, error)
	Fetch(ctx context.Context, loc domain.Location, opts FetchOptions) error
	MergeFastForwardOnly(ctx context.Context, loc domain.Location, target string) error
}

var _ Interface = (*Client)(nil)
