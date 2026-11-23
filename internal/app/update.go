package app

import (
	"context"
	"fmt"
	"time"

	"github.com/mmo-con/headquarters/internal/domain"
	"github.com/mmo-con/headquarters/internal/git"
	"github.com/mmo-con/headquarters/internal/jobs"
)

// ReasonUpToDate is the skip reason of a repository that needs no update.
const ReasonUpToDate = "already up to date"

// Eligibility checks every safety condition of the safe update. It returns an
// empty reason if the repository can be fast-forwarded.
func Eligibility(r domain.Repository, storeBusy bool) string {
	switch {
	case !r.IsAvailable():
		return "repository is " + r.Availability.String()
	case r.Inspected.Err != nil || !r.Head.Valid || !r.Worktree.Valid || !r.Upstream.Valid || !r.Operation.Valid:
		return "state is unknown"
	case r.Operation.Value != domain.NoOperation:
		return r.Operation.Value.String() + " in progress"
	case r.Worktree.Value.HasTrackedChanges():
		return "worktree contains local changes"
	case r.Head.Value.Detached:
		return "detached HEAD"
	case r.Head.Value.Unborn:
		return "branch has no commits"
	case !r.Upstream.Value.Configured:
		return "no upstream"
	case r.Upstream.Value.Gone:
		return "upstream gone"
	case r.Upstream.Value.Diverged():
		return "diverged from upstream"
	case r.Upstream.Value.Ahead > 0:
		return "ahead of upstream"
	case r.Upstream.Value.Behind == 0:
		return ReasonUpToDate
	case r.Upstream.Value.CommitID == "":
		return "upstream commit is unknown"
	case storeBusy:
		return "another operation or lazygit session is active"
	}
	return ""
}

// UpdateCandidate is one row of the update preview.
type UpdateCandidate struct {
	Repository domain.Repository
	SkipReason string
}

// PlanUpdate splits targets into eligible and skipped repositories.
func PlanUpdate(targets []domain.Repository, storeBusy func(domain.StoreID) bool) (eligible, skipped []UpdateCandidate) {
	for _, r := range targets {
		reason := Eligibility(r, r.StoreID != "" && storeBusy(r.StoreID))
		if reason == "" {
			eligible = append(eligible, UpdateCandidate{Repository: r})
		} else {
			skipped = append(skipped, UpdateCandidate{Repository: r, SkipReason: reason})
		}
	}
	return eligible, skipped
}

// UpdateJobs creates fast-forward jobs. Each job inspects the repository again
// while holding the store lock and skips it if any condition no longer holds
// or if the state differs from the preview the user confirmed.
func UpdateJobs(client git.Interface, all []domain.Repository, eligible []UpdateCandidate, timeout time.Duration) []jobs.Job {
	var out []jobs.Job
	for _, c := range eligible {
		r := c.Repository
		out = append(out, jobs.Job{
			Kind:         jobs.KindUpdate,
			Operation:    domain.OperationUpdate,
			RepositoryID: r.ID,
			Repositories: StoreMembers(all, map[domain.StoreID]bool{r.StoreID: true}),
			StoreID:      r.StoreID,
			Mutating:     true,
			Timeout:      timeout,
			Run: func(ctx context.Context) (any, error) {
				in, err := client.Inspect(ctx, r.Location, git.InspectOptions{Untracked: domain.UntrackedNo})
				if err != nil {
					return nil, fmt.Errorf("recheck failed: %w", err)
				}
				fresh := r
				fresh.ApplyInspection(fresh.Inspected.Generation, in, nil, time.Now())
				if reason := Eligibility(fresh, false); reason != "" {
					return nil, &jobs.SkipError{Reason: "update skipped: " + reason}
				}
				if change := changedSincePreview(r, fresh); change != "" {
					return nil, &jobs.SkipError{Reason: "update skipped: " + change + " since the update was planned; press u to try again"}
				}
				return nil, client.MergeFastForwardOnly(ctx, r.Location, r.Upstream.Value.CommitID)
			},
		})
	}
	return out
}

// changedSincePreview names the part of the state that differs between the
// confirmed preview and a fresh inspection, or returns "".
func changedSincePreview(preview, fresh domain.Repository) string {
	ph, fh := preview.Head.Value, fresh.Head.Value
	pu, fu := preview.Upstream.Value, fresh.Upstream.Value
	switch {
	case ph.Branch != fh.Branch:
		return "branch changed"
	case ph.CommitID != fh.CommitID:
		return "HEAD moved"
	case pu.Ref != fu.Ref:
		return "upstream changed"
	case pu.CommitID != fu.CommitID:
		return "upstream moved"
	}
	return ""
}
