# ADR 0019: Deleting squash- and rebase-merged branches

Status: Accepted

## Context

[ADR 0018](0018-deleting-merged-branches.md) deletes only branches whose
commit is an ancestor of the default branch, with `git branch -d`. Pull
requests merged with a squash or rebase merge put new commits on the
default branch, so their local branches are never deleted; in a repository
that merges only that way, the cleanup deletes nothing. A branch whose
upstream is gone but that was merged by ancestry also needed a safe update
first, because `git branch -d` checks HEAD.

Comparing patches, for example with `git cherry`, was rejected: patch IDs
ignore whitespace, so a change of indentation could count as merged.

## Decision

A local branch whose upstream is gone is deleted if all of its changes are
in the default branch:

- its commit is an ancestor of the default branch, as in ADR 0018; or
- merging it into the default branch would change nothing:

  ```text
  git merge-tree --write-tree --no-messages <default commit> <branch commit>
  ```

  succeeds without conflicts, and the resulting tree is the tree of the
  default branch, byte for byte.

A conflict, another tree, or a merge that Git refuses, such as one of
unrelated histories, keeps the branch with the reason `upstream gone,
changes not in <default>`. The check runs only for branches with a gone
upstream, the sign that the branch was removed on the server, and only when
the preview opens: one process per such branch. It can write the objects of
a differing merge result; they are unreferenced and removed by Git's
garbage collection.

`merge-tree --write-tree` needs Git 2.38. With older Git down to the minimum
of [ADR 0008](0008-compatibility-performance-and-testing.md), such branches
are kept with a reason that names Git 2.38.

These branches, and gone branches merged into the default branch but not
into HEAD, are deleted with a compare-and-delete of the checked commit,
after the recheck of ADR 0018 under the store lock:

```text
git update-ref -d refs/heads/<branch> <checked commit>
```

`git branch -d` would refuse them, because it checks HEAD or the ancestry
only. Git deletes the ref only if it still points to the checked commit.
Every other candidate is deleted with `git branch -d` as before; branches
without an upstream keep the reason `update <branch> first`.

## Consequences

- The cleanup deletes branches of squash and rebase merges, the usual
  merge methods on several hosting services, and gone branches no longer
  need a safe update first.
- The guarantee is no longer Git's own merge check but the containment
  check above, which compares exact content. A squash merge whose lines
  were changed again on the default branch is kept and stays with lazygit.
- The ref and its reflog are removed; the content stays in the default
  branch.
