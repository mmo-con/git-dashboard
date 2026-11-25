# ADR 0013: Calmer table and detail panel

Status: Accepted

## Context

[ADR 0007](0007-user-interface-principles.md) defined the table, the header,
the detail panel, and the family rows of the tree. In use, several parts
were hard to read or restless:

- Path and family stood untitled at the top of the detail panel, while the
  source of the repository sat far away in the bottom border. Branch and
  worktree each took a titled section for a single line, and every section
  title had a rule across the panel.
- The fetch age stood below the recent commits and looked like another
  commit, although it belongs to the upstream state. The result of the
  latest fetch or update was visible only if it failed.
- Family names were bold like the names of all repositories that need
  attention, and only three columns further left. The health summary of a
  family started right after its name, at a different position in every
  row, and added text even to healthy and expanded families.
- Header counters with matches were colored boxes, so several matches
  formed a bright bar.
- The Commit column repeated a full date and time in every row, the State
  column repeated `diverged` next to red ahead and behind counts, and the
  Family column repeated the same family under a family filter.
- A stale or missing fetch was visible only in the detail panel, so the
  table could show outdated ahead and behind counts without a hint.

## Decision

The detail panel has labeled lines in sections titled with a short rule
prefix (`── STATUS`):

- **Status** comes first: the branch with its upstream and relation, the
  fetch age, the latest fetch or update of this session with its result and
  age, an operation in progress, the worktree, and stash entries.
- Errors, other branches, recent commits, remotes, and linked worktrees
  follow. Remote names take the label column; linked worktree paths are
  shortened to `~`.
- **Repository** comes last: the path and workspace with the home directory
  shortened to `~`, the family, and the source (configured or discovered).
  These values do not change while the repository is shown.

In the tree, a family row puts its toggle in the mark column, directly after
the cursor, so the family name starts left of the tree guides. The name uses
the title color. Only collapsed families show a health summary; it starts in
the State column, or after the name if the name reaches into that column. A
healthy family shows only the healthy symbol.

In the table:

- Only the names of repositories with errors or critical states are bold;
  this amends "only healthy names are not emphasized" in ADR 0007.
- The Commit column shows the age of the latest commit: `now`, minutes,
  hours up to two days, days up to two months, then months and years, such
  as `15 h` or `21 d`. The detail panel keeps the absolute time. This
  amends the common commit time format of ADR 0007.
- The State column no longer names divergence; the ahead and behind counts
  in the error color and the critical health symbol show it.
- The Family column is hidden under a family filter, as in the tree.

Header counters with matches are text in their state color; counters
without matches are dim. Only the active quick filter has a background and
is bracketed.

A repository with remotes that was never fetched, or not fetched for more
than seven days, has a stale fetch. It shows `never fetched` or
`stale fetch` in the State column, counts as other attention highlighted as
a warning, and has a header counter and the quick filter `is:stale`. An
unknown fetch time is not reported. The Commit column stays: it compares
the latest activity across repositories and backs sorting by commit time.

## Consequences

- The detail panel reads as label–value pairs; status changes are at the
  top and static values at the bottom, where a small panel cuts off first.
- Family rows differ from repository rows by position, color, and toggle,
  and their summaries line up with the repositories' State column. An
  expanded family has no summary; its rows show the health.
- Bold names, colored header text, and short ages leave emphasis to errors,
  critical states, and the active filter.
- Relative ages change over time; the periodic redraw of
  [ADR 0012](0012-no-persistent-state-and-no-automatic-activity.md) keeps
  them current without starting a process.
- A freshly cloned repository has a stale fetch until its first fetch,
  because Git writes `FETCH_HEAD` only when fetching.
- Concrete symbols, columns, and filters are listed in
  [the key binding reference](../keybindings.md).
