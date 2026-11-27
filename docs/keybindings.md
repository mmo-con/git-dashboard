# Key bindings

The help view (`?`) shows these bindings in short form together with a legend
of the health column and the status symbols.

## Navigation

| Key | Action |
|---|---|
| `j` / `↓` | next row |
| `k` / `↑` | previous row |
| `g` / `G` | first / last row |
| `PgUp` / `PgDn` | page up / down |
| `h` / `←` | in the tree: collapse the family; on a repository, select its family row and collapse it |
| `l` / `→` | in the tree: expand the family |
| `[` / `]` | in the tree: previous / next family row |

The repository list keeps up to two rows of context above and below the
cursor when space and list bounds allow. When you reverse direction, the
cursor moves within the current view before the list scrolls again.

## View

| Key | Action |
|---|---|
| `/` | text filter |
| `1` … `8` | quick filter: dirty, behind, ahead, diverged, errors, other branches, stashed, stale fetch; press again to clear |
| `Esc` | clear the text filter or close the current overlay |
| `Tab` / `Shift+Tab` | next / previous family |
| `t` | search and choose a family |
| `v` | switch between the flat list and the family tree |
| `z` | in the tree: collapse all families if any is expanded, otherwise expand all |
| `s` | choose sorting: name, family, latest commit, worktree state, upstream state, or health; name and path break ties |
| `r` | refresh visible repository state |
| `R` | reload configuration, rebuild inventory, refresh all; a new Git executable, parallelism limits, lazygit, and launchers take effect at once. When started with a path, the same path is scanned again |
| `e` | show error details and warnings |
| `b` | show the operations view |
| `?` | help |
| `q` / `Ctrl+C` | quit; while a fetch or update batch runs, asks first, because quitting cancels it |

The header shows the quick filters with their key and the number of
matching repositories in the current family. A counter with matches is
text in its state color, a counter without matches is dimmed, and the
active quick filter has a background and is bracketed, for example
`[1 dirty 3]`, so it is recognizable without colors.

## Repository

| Key | Action |
|---|---|
| `Enter` | open selected repository in lazygit, or expand/collapse a selected family row |
| `o` | open launcher menu |
| `y` | copy the worktree path, branch name, primary remote URL, or its web URL to the clipboard |

The copy menu shows each value before it is copied. An entry is unavailable,
with the reason, when the value does not exist or is unknown: a detached
HEAD has no branch, and a repository may have no remote or not yet be
inspected. An unborn HEAD copies the branch it will create. Remote URLs are
copied without user information such as `user:token@`. Values that contain
control characters are never copied. The primary remote is the configured
`remote_name`, else `origin`, else the alphabetically first remote. The web
URL is the HTTPS address that *Open remote in browser* opens, also for SSH
remotes; see [configuration](configuration.md) for how it is derived.

headquarters copies with `pbcopy` on macOS and with `wl-copy` (Wayland),
`xclip`, or `xsel` (X11) on Linux, trying them in this order. Without a
usable tool, for example in an SSH session without a display, copying
reports an error; the terminal clipboard (OSC 52) is not used.

## Batch

| Key | Action |
|---|---|
| `Space` | toggle mark; on a family row, mark its matching repositories, or unmark them if all are marked |
| `a` | mark all visible repositories |
| `A` | clear all marks |
| `f` | fetch marked repositories, or the repository or family under the cursor if none are marked |
| `F` | fetch visible repositories |
| `u` | safe update of marked repositories, or the repository or family under the cursor if none are marked |
| `U` | safe update of visible repositories |

A fetch starts at once. An update asks for confirmation first.

The view at startup is set with `[ui].view` in the configuration, the sort
order with `[ui].sort`. In the
tree, families are listed alphabetically, with the current sort order
applied within each family. Filters still apply. Families start collapsed and
retain their expansion state when you switch between views. While a text or
quick filter is active, every family with matches is expanded; expanding or
collapsing during the filter lasts until the filter is cleared, which
restores the previous state. The family filter does not expand families.
A collapsed family row summarizes the health of its matching repositories in
the State column, for example `✗1 error  ●2 dirty`, or shows `✓` if all are
healthy; an expanded family row has no summary.
`h`, `l`, `z`, `[`, and `]` act only in the tree.

`a`, `F`, and `U` act only on matching repositories in expanded families,
regardless of marks. Rows outside the viewport still count. Marks persist
when filters change or families collapse. A batch on marked repositories
includes all of them; the update confirmation and the fetch message state
how many are hidden. Without marks, `f` and `u` on a family row act on all
its matching repositories, also when the family is collapsed.
The repository panel shows the total number of marks and the `A` shortcut
whenever any marks are set, including when all marked rows are hidden.

## Update confirmation

| Key | Action |
|---|---|
| `j` / `k` | move |
| `Enter` / `y` | fetch, then update |
| `Esc` / `n` | cancel |

The confirmation lists the repositories with their state before the fetch,
including the number of incoming upstream commits as of the last fetch
(`↓3`, or `↓?` if the upstream state is unknown). Below the list, it shows
the incoming commits of the selected repository, up to
`[ui].incoming_commits`. The update fetches again, so it can bring more
commits than listed. After confirming, the operations view shows the fetch; once it and the
following inspection are finished, the update starts on its own with the
fetched state. Repositories whose fetch failed are skipped with the fetch
error, even if they looked eligible before. Closing the operations view does
not stop the update; it then runs without opening the view again. `c`
cancels the fetch and with it the update.

A repository that changed after the update was planned, for example because
another tool fetched new upstream commits, is skipped with
`… since the update was planned; press u to try again`.

## Operations view

| Key | Action |
|---|---|
| `j` / `k` | select a row; failed rows show their stderr, rows with incoming commits list them |
| `Enter` | open the repository of the selected row in lazygit; the view stays open |
| `c` | cancel the running batch, or the update that waits for its fetch |
| `x` | retry failed and cancelled operations |
| `u` | update batch: confirm a new update of every repository that was not updated |
| `t` | retry the selected failed fetch in the terminal |
| `Esc` / `b` | close the view; the batch continues |

An update row lists the commits its fast-forward applies, as planned after
the fetch, also once the update is finished. A fetch row lists the incoming
commits of its repository after the fetch.

An update batch also lists the repositories that the update skipped, except
those that were already up to date, so that every blocked repository can be
opened from the list. After a repository is inspected again, its row shows
`→ now eligible` or `→ now up to date`; after a lazygit session opened from
the row, it shows `→ still blocked: <reason>` if the blocker remains. Nothing
is updated until the new update is confirmed.

## Error view

| Key | Action |
|---|---|
| `t` | fetch the selected repository in the terminal, for example to enter a passphrase |

## Help view

| Key | Action |
|---|---|
| `j` / `k` | scroll |
| any other key | close |

## Table columns

| Column | Content |
|---|---|
| (untitled) | health symbol: the most important state of the row |
| Repository | name |
| Family | family of the repository; hidden in the tree, whose family rows name it, and under a family filter |
| Branch | checked-out branch, or the commit of a detached HEAD |
| State | what the counts do not show: an unavailable entry, a queued, running, or failed fetch or update, unknown state, an operation in progress, conflicts, no commits, a detached HEAD, no or a gone upstream, a remote mismatch, a repository with remotes that was never fetched or not within seven days (`never fetched`, `stale fetch`), and other branches; empty when there is nothing to report |
| Staged | staged entries |
| Modified | entries modified or deleted in the worktree and not staged |
| Untracked | untracked entries |
| Stashed | number of stash entries of the repository; linked worktrees share them |
| Ahead / Behind | commits ahead of / behind the upstream as of the last fetch; both in the error color if the branch diverged |
| Commit | age of the latest commit: `now`, minutes, hours up to two days, days up to two months, then months and years, such as `15 h` or `21 d`; the commit lists of the detail panel show the same age |

In the count columns, `–` is a known zero and `?` an unknown value. A cell
is empty while the repository loads and where the count does not apply,
such as ahead and behind without an upstream; the State column names the
reason. `off` means that untracked files are not checked (`untracked = "no"`).
If the table is too narrow for the full titles, the count columns are
titled with their symbols, for example `✚` for Staged.

## Status symbols

| Unicode | ASCII | Meaning |
|---|---|---|
| `✓` | `ok` | clean worktree or up to date; the detail panel shows `✓ clean` |
| `✚n` | `+n` | staged entries |
| `●n` | `*n` | modified entries |
| `−n` | `-n` | deleted entries |
| `?n` | `?n` | untracked entries |
| `✘n conflicts` | `!n conflicts` | conflicting entries |
| `↑n` / `↓n` | `^n` / `vn` | commits ahead of / behind the upstream |
| `⎇n` | `brn` | other local branches that need attention |
| `≡n` | `$n` | stash entries |
| `≠remote` | `!=remote` | remote URL differs from the configured `remote` |
| `✗` | `x` | unavailable entry or failed fetch |
| `◆` | `*` | marked for batch operations |
| `▸` / `▾` | `[+]` / `[-]` | collapsed / expanded family in the tree |

## Health column

The untitled first column shows only the most important state of the row,
by priority from top to bottom; the other columns and the detail panel show
the rest.

| Unicode | ASCII | Meaning |
|---|---|---|
| `✗` | `x` | error or unavailable entry |
| `✘` | `!` | conflict, operation in progress, or divergence |
| `●` | `*` | local changes in the worktree |
| `↓` | `v` | behind the upstream |
| `↑` | `^` | ahead of the upstream |
| `!` | `~` | other attention: other branches, stash entries, a remote mismatch, a detached HEAD, a gone upstream, or a stale fetch; the State column names it |
| `✓` | `ok` | healthy |
| empty | empty | not inspected yet |

Sorting by health uses the same priority, most urgent first. Repositories
whose state is unknown, including those not inspected yet, come after other
attention and before healthy repositories.

## Filter syntax

Press `t` and type part of a family name to narrow the family picker. Use
`↑` / `↓` and `Enter` to select, or `Esc` to cancel. Choose **All families**
to remove the family filter. The `/` text filter and quick filters still
apply to the selected family.

The text filter consists of space-separated tokens that must all match.
Plain tokens match the name or the path case-insensitively. `family:<name>`
matches the family name exactly, ignoring case, for example `family:work`;
family names with spaces cannot be written as a token yet. It combines with
the family picker. State tokens:

| Token | Matches |
|---|---|
| `is:dirty` / `is:clean` | worktree with or without entries |
| `is:behind` / `is:ahead` / `is:diverged` | upstream state |
| `is:noupstream` | no upstream or upstream gone |
| `is:error` | configuration, inspection, or validation errors |
| `is:unavailable` | missing, invalid, or unsupported entries |
| `is:mismatch` | expected remote differs |
| `is:marked` | marked repositories |
| `is:branches` | other local branches need attention |
| `is:unpublished` | another local branch has no upstream, a gone upstream, unpushed commits, or is missing |
| `is:stashed` | the repository has stash entries |
| `is:stale` | the repository has remotes and was never fetched or not within seven days |
