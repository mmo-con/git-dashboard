# Key bindings

The help view (`?`) shows the same list.

## Navigation

| Key | Action |
|---|---|
| `j` / `↓` | next row |
| `k` / `↑` | previous row |
| `g` / `G` | first / last row |
| `PgUp` / `PgDn` | page up / down |

## View

| Key | Action |
|---|---|
| `/` | text filter |
| `Esc` | clear the text filter or close the current overlay |
| `Tab` / `Shift+Tab` | next / previous family |
| `s` | choose sorting |
| `r` | refresh visible repository state |
| `R` | reload configuration, rebuild inventory, refresh all |
| `e` | show error details and warnings |
| `b` | show the operations view |
| `?` | help |
| `q` / `Ctrl+C` | quit |

## Repository

| Key | Action |
|---|---|
| `Enter` | open selected repository in lazygit |
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

Marks persist when filters change. A batch on marked repositories includes
all of them; the confirmation states how many are hidden by the filter.

## Operations view

| Key | Action |
|---|---|
| `j` / `k` | select a row; failed rows show their stderr |
| `c` | cancel the running batch |
| `x` | retry failed and cancelled operations |
| `t` | retry the selected failed fetch in the terminal |
| `Esc` / `b` | close the view; the batch continues |

## Error view

| Key | Action |
|---|---|
| `t` | fetch the selected repository in the terminal, for example to enter a passphrase |

## Filter syntax

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
