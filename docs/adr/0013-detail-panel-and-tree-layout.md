# ADR 0013: Detail panel and tree layout

Status: Accepted

## Context

[ADR 0007](0007-user-interface-principles.md) defined the detail panel and
the family rows of the tree. In use, several parts were hard to read:

- Path and family stood untitled at the top of the detail panel, while the
  source of the repository sat far away in the bottom border.
- Branch and worktree each took a titled section for a single line.
- The fetch age stood below the recent commits and looked like another
  commit, although it belongs to the upstream state.
- Family names were bold like the names of repositories that need
  attention, and only three columns further left.
- The health summary of a family started right after its name, at a
  different position in every row, and added text even to healthy families
  and to expanded families whose rows already show their health.
- A stale or missing fetch was visible only in the detail panel, so the
  table could show outdated ahead and behind counts without a hint.

## Decision

The detail panel has labeled lines in titled sections:

- **Status** comes first: the branch with its upstream and relation, the
  fetch age, an operation in progress, the worktree, and stash entries.
- Errors, other branches, recent commits, remotes, and linked worktrees
  follow as before.
- **Repository** comes last: the path and workspace with the home directory
  shortened to `~`, the family, and the source (configured or discovered).
  These values do not change while the repository is shown.

In the tree, a family row puts its toggle in the mark column, directly after
the cursor, so the family name starts left of the tree guides. The name uses
the title color. Only collapsed families show a health summary; it starts in
the State column, or after the name if the name reaches into that column. A
healthy family shows only the healthy symbol.

A repository with remotes that was never fetched, or not fetched for more
than seven days, shows `never fetched` or `stale fetch` in the State column
and counts as other attention, highlighted as a warning. An unknown fetch
time is not reported. The Commit column stays: it compares the latest
activity across repositories and backs sorting by commit time.

## Consequences

- The detail panel reads as label–value pairs; status changes are at the
  top and static values at the bottom, where a small panel cuts off first.
- Family rows differ from repository rows by position, color, and toggle,
  and their summaries line up with the repositories' State column.
- An expanded family has no summary; its rows show the health.
- A freshly cloned repository counts as never fetched until its first
  fetch, because Git writes `FETCH_HEAD` only when fetching.
- Concrete symbols and columns are listed in
  [the key binding reference](../keybindings.md).
