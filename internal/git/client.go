package git

import (
	"context"

	"github.com/mmo-con/headquarters/internal/domain"
)

// Client is the typed wrapper around the official Git CLI.
type Client struct {
	run runner
	// stashHeader is set by CheckVersion if status reports the stash count in
	// porcelain v2 (Git 2.35); otherwise a separate process counts it.
	stashHeader bool
	// mergeTree is set by CheckVersion if merge-tree --write-tree (Git 2.38)
	// is available.
	mergeTree bool
}

// New returns a client that runs the given Git executable without checking
// its version; use Open outside of tests.
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
	RecentCommits(ctx context.Context, loc domain.Location, n int) ([]domain.CommitSummary, error)
	IncomingCommits(ctx context.Context, loc domain.Location, head, upstream string, n int) ([]domain.CommitSummary, error)
	ReadBranchCleanup(ctx context.Context, loc domain.Location, remote string) (BranchCleanup, error)
	DeleteBranch(ctx context.Context, loc domain.Location, name string) error
	DeleteBranchRef(ctx context.Context, loc domain.Location, name, commit string) error
}

var _ Interface = (*Client)(nil)
