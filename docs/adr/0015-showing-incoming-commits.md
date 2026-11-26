# ADR 0015: Showing incoming commits

Status: Accepted

## Context

The safe update of [ADR 0004](0004-batch-fetch-and-safe-update.md)
fast-forwards repositories after one confirmation. The confirmation stated
only the intent and the state before the fetch, and the detail panel listed
only local commits. To review the upstream commits an update would bring,
users had to open every repository in lazygit first
([#17](https://github.com/mmo-con/headquarters/issues/17)).

## Decision

Incoming commits are the commits reachable from the upstream commit but not
from HEAD, as of the last inspection. They are listed read-only from local
objects; listing them never fetches:

```text
git log --max-count=<n> --no-color --no-show-signature --format=%H%x00%h%x00%an%x00%ct%x00%s <upstream commit> ^<HEAD commit> --
```

The command uses the commit IDs from the inspection or the update plan
rather than `@{upstream}`, so the list matches the shown or planned state.
Their total is the behind count of the inspection; no process counts them.
`[ui].incoming_commits` (0–50, default 10) limits the list; further commits
are summarized as `… and n more`. With `0`, only the counts are shown and no
process runs.

Like the recent commits of [ADR 0003](0003-git-cli-and-process-isolation.md),
a read-only job in the inspection pool, with the inspection timeout and no
lock, reads them only for the selected item: one request at a time,
cancelled when the selection moves on, and cached by repository, HEAD
commit, upstream commit, and limit. A failed request is read again after a
refresh.

They are shown:

- in the detail panel, below the recent commits, for a branch behind its
  upstream, also when diverged; not when up to date, without upstream, or
  with the upstream gone. When the panel is small, older recent commits are
  dropped first, then incoming commits down to the newest;
- in the update confirmation: every row states its incoming count, `↓?` if
  the upstream state is unknown, never zero; below the list, the commits of
  the selected repository. This amends "the commit list is not yet known"
  in ADR 0004: the confirmation shows the commits as of the last fetch, and
  the update still fetches before it plans and may bring more;
- in the operations view, for the selected row: an update row keeps the
  range it was planned with after the fetch, so it lists the commits its
  fast-forward applies also after HEAD has moved; a fetch row lists the
  incoming commits of its repository after the reinspection.

The update does not pause between its fetch and the fast-forward; reviewing
the fetched commits before applying them would need a second confirmation.

## Consequences

- Users can review incoming changes without leaving the dashboard, and see
  what an update applied.
- Moving through a list starts at most one additional Git process at a time,
  which only reads local objects. This amends the list of inspections in
  [ADR 0012](0012-no-persistent-state-and-no-automatic-activity.md).
- Before the update's fetch, the list can be incomplete; the operations view
  shows the commits that were actually applied.
