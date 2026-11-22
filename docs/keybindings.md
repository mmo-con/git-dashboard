# Key bindings

The help view (`?`) shows these bindings in short form together with a legend
of the status symbols.

## Navigation

| Key | Action |
|---|---|
| `j` / `↓` | next row |
| `k` / `↑` | previous row |
| `g` / `G` | first / last row |
| `PgUp` / `PgDn` | page up / down |

The repository list keeps up to two rows of context above and below the
cursor when space and list bounds allow. When you reverse direction, the
cursor moves within the current view before the list scrolls again.

## View

| Key | Action |
|---|---|
| `/` | text filter |
| `1` … `6` | quick filter: dirty, behind, ahead, diverged, errors, other branches; press again to clear |
| `Esc` | clear the text filter or close the current overlay |
| `Tab` / `Shift+Tab` | next / previous family |
| `t` | search and choose a family |
| `v` | switch between the flat list and the family tree |
| `s` | choose sorting |
| `r` | refresh visible repository state |
| `R` | reload configuration, rebuild inventory, refresh all; a new Git executable, parallelism limits, lazygit, and launchers take effect at once |
| `e` | show error details and warnings |
| `b` | show the operations view |
| `?` | help |
| `q` / `Ctrl+C` | quit; while a fetch or update batch runs, asks first, because quitting cancels it |

## Repository

| Key | Action |
|---|---|
| `Enter` | open selected repository in lazygit, or expand/collapse a selected family row |
| `o` | open launcher menu |

## Batch

| Key | Action |
|---|---|
| `Space` | toggle mark |
| `a` | mark all visible repositories |
| `A` | clear all marks |
| `f` | fetch marked repositories, or the repository under the cursor if none are marked |
| `F` | fetch visible repositories |
| `u` | safe-update preview for marked repositories, or visible ones if none are marked |

The view at startup is set with `[ui].view` in the configuration. In the
tree, families are listed alphabetically, with the current sort order
applied within each family. Filters still apply. Families start collapsed and
retain their expansion state when you switch between views.

`a` and `F` act only on matching repositories in expanded families, as
does `u` when there are no marks. Rows outside the viewport still count.
Marks persist when filters change or families collapse. A batch on marked
repositories includes all of them; the confirmation states how many are hidden.
The repository panel shows the total number of marks and the `A` shortcut
whenever any marks are set, including when all marked rows are hidden.

## Update preview

| Key | Action |
|---|---|
| `j` / `k` | move |
| `Enter` / `y` | start the update |
| `f` | fetch the repositories of the preview, then show the preview again |
| `Esc` / `n` | cancel |

`f` shows the fetch in the operations view. When the fetch and the following
inspection are finished, the preview opens again with the fetched state;
repositories whose fetch failed are skipped. Nothing is updated until the new
preview is confirmed. A cancelled fetch does not reopen the preview.

## Operations view

| Key | Action |
|---|---|
| `j` / `k` | select a row; failed rows show their stderr |
| `Enter` | open the repository of the selected row in lazygit; the view stays open |
| `c` | cancel the running batch |
| `x` | retry failed and cancelled operations |
| `u` | update batch only: open a new safe-update preview for every repository that was not updated |
| `t` | retry the selected failed fetch in the terminal |
| `Esc` / `b` | close the view; the batch continues |

An update batch also lists the repositories that the preview skipped, except
those that were already up to date, so that every blocked repository can be
opened from the list. After a repository is inspected again, its row shows
`→ now eligible` or `→ now up to date`; after a lazygit session opened from
the row, it shows `→ still blocked: <reason>` if the blocker remains. Nothing
is updated until the new preview is confirmed.

## Error view

| Key | Action |
|---|---|
| `t` | fetch the selected repository in the terminal, for example to enter a passphrase |

## Help view

| Key | Action |
|---|---|
| `j` / `k` | scroll |
| any other key | close |

## Status symbols

| Unicode | ASCII | Meaning |
|---|---|---|
| `✓` | `ok` | clean worktree; the detail panel shows `✓ clean` |
| `✚n` | `+n` | staged entries |
| `●n` | `*n` | modified entries |
| `−n` | `-n` | deleted entries |
| `?n` | `?n` | untracked entries |
| `✘n` | `!n` | conflicting entries |
| `↑n` / `↓n` | `^n` / `vn` | commits ahead of / behind the upstream |
| `✓` | `ok` | up to date with the upstream |
| `⎇n` | `brn` | other local branches that need attention |
| `≠remote` | `!=remote` | remote URL differs from the configured `remote` |
| `!` | `~` | health symbol: other attention, such as a remote mismatch or other branches |
| `✗` | `x` | unavailable entry or failed fetch |
| `◆` | `*` | marked for batch operations |
| `▸` / `▾` | `[+]` / `[-]` | collapsed / expanded family in the tree |

The symbol before the repository name repeats the most important state of
the row: `✗` error or unavailable, `✘` conflict,
operation in progress, or divergence, `●` dirty worktree, `↓` behind, `↑`
ahead, `!` other attention, and `✓` healthy.

## Filter syntax

Press `t` and type part of a family name to narrow the family picker. Use
`↑` / `↓` and `Enter` to select, or `Esc` to cancel. Choose **All families**
to remove the family filter. The `/` text filter and quick filters still
apply to the selected family.

The text filter consists of space-separated tokens that must all match.
Plain tokens match the name or the path case-insensitively. State tokens:

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
