# ADR 0020: Branches of rebases and bisects

Status: Accepted

## Context

[ADR 0019](0019-deleting-squash-and-rebase-merged-branches.md) deletes some
branches with a compare-and-delete of the checked commit,
`git update-ref -d`, instead of `git branch -d`. `git branch -d` and `-D`
also refuse a branch that a worktree uses: checked out, being rebased, or
being bisected. While a rebase or bisect runs, the worktree's HEAD is
detached, so `%(worktreepath)` does not name the branch and the preview
did not list it as checked out. A review found that a squash-merged branch
in the middle of an interactive rebase was deleted; `git rebase --continue`
then failed with `could not update refs/heads/<branch>`, and the rebased
commits were left on a detached HEAD.

## Decision

While a rebase or bisect is in progress in any worktree of the store, every
branch that would be deleted with `git update-ref -d` is kept with the
reason `rebase in progress; finish it first` or
`bisect in progress; finish it first`. Branches deleted with
`git branch -d` are not affected, because Git checks them itself.

The state is read without a Git process, from the file system: the
`rebase-merge`, `rebase-apply`, and `BISECT_LOG` markers of the main
worktree in the common Git directory and of every linked worktree in
`<common dir>/worktrees/<id>`, the layout documented in
gitrepository-layout(5). Worktrees that are not entries of the inventory
count as well. A marker that cannot be checked fails the read instead of
counting as no operation.

The rule applies to the preview and to the recheck under the store lock, so
a rebase started after the preview also keeps the branch.

## Consequences

- A compare-and-delete never removes a branch that a rebase or bisect
  still needs.
- The rule is coarser than Git's, which checks the branch each operation
  uses: while one worktree rebases, squash-merged branches of the whole store
  are kept. Reading which branch an operation uses would mean parsing Git's
  internal state files, which [ADR 0003](0003-git-cli-and-process-isolation.md)
  rules out. Finishing or aborting the operation and pressing `d` again
  deletes them.
- The preview starts no further process; its cost stays as in
  [ADR 0018](0018-deleting-merged-branches.md) and ADR 0019.
