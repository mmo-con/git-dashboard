# headquarters — Architecture and Decisions

This document explains how headquarters works and why. It records the
contracts with Git, the safety rules, and the decisions behind them. What the
user configures and which keys exist is described in
[configuration.md](configuration.md) and [keybindings.md](keybindings.md);
this document does not repeat it.

## 1. Purpose and scope

headquarters manages a curated collection of local Git repositories: it shows
their state side by side, groups them into families, runs safe batch
operations such as fetch and fast-forward updates, and hands all detailed work
within one repository over to lazygit.

> headquarters manages the repository collection. lazygit manages the
> individual repository.

Non-goals. headquarters does not:

- implement a Git user interface, create commits, or resolve conflicts;
- merge, rebase, stash, reset, or force-push on its own;
- implement Git protocols, credentials, or SSH;
- embed or fork lazygit;
- run arbitrary commands across repositories (unlike gita, myrepos, or
  mu-repo); ghq-style cloning and lazygit's single-repository work stay with
  those tools;
- support Windows.

## 2. Repository inventory

### 2.1 Hybrid inventory

Repositories enter the dashboard in two ways: explicit `[[repositories]]`
entries and bounded `[[discovery]]` below root directories. Explicit entries
give deterministic control over important repositories; discovery avoids
maintaining long lists for less stable areas. The precedence of both is
specified in [configuration.md](configuration.md#precedence): an explicit
entry always wins, and `enabled = false` suppresses a single repository.

Discovery is bounded on purpose. An unrestricted scan would traverse
dependency and build trees, pick up unrelated repositories, change its result
whenever something is cloned below a root, and make startup cost
unpredictable.

A path on the command line (`headquarters ~/src`) replaces both with one
discovery below that path, using the discovery defaults. It lets a user look
at a directory without writing an inventory first. Only the inventory is
replaced; the remaining settings of the configuration file still apply, so
the theme, lazygit, and launchers behave as usual. The path is kept across
`R`, which never switches back to the configured inventory.

### 2.2 Discovery rules

- The walk goes up to `max_depth` levels below `root` (the root has depth 0).
- A directory with a `.git` directory or file is a candidate and is validated
  through Git (see 3.5). Other directories are not errors.
- The walk never descends into a repository. Nested repositories, submodules,
  and vendored checkouts enter only through explicit entries. Bare
  repositories have no `.git` entry and are never discovered.
- Patterns use doublestar syntax against the slash-separated path relative to
  the root; matching is case-sensitive everywhere. An excluded directory is
  pruned, so large trees such as `node_modules` cost nothing. A pattern ending
  in `/**` also matches the directory itself.
- Symbolic links are followed only with `follow_symlinks = true`; directories
  already visited (device and inode) are skipped.
- Families are assigned explicitly or default to `Ungrouped`; they are never
  derived from directory names. If several roots find a repository, the first
  root in configuration order supplies the defaults, with a warning.

### 2.3 Identity and repository stores

- An entry's identity is its canonical worktree root: absolute, `~` and
  `${NAME}` expanded, symbolic links resolved. Deduplication also compares
  with `os.SameFile`, which covers case-insensitive file systems and aliases
  such as `/var` and `/private/var`.
- An explicit `path` must be the worktree root reported by Git. A
  subdirectory is shown as `config error` naming the actual root; it is never
  normalized silently.
- Linked worktrees are separate entries. Entries with the same common Git
  directory form a **repository store**. Locks and fetches act per store.
- Bare repositories are not supported as entries.

### 2.4 Local paths, not URLs

The local path is the identity; the repository must already be checked out.
A configured `remote` is only an assertion: a mismatch is reported, never
fixed, because repositories may have several remotes, SSH and HTTPS URLs may
name the same repository, and host aliases are environment-specific.
Automatic cloning is not implemented; if it is ever added, it must be an
explicit user action through the Git CLI.

## 3. Git integration

### 3.1 One backend: the official Git CLI

All Git information and operations come from the installed `git` executable;
`go-git` or any other implementation is not used. Mixing implementations
would let status, fetch, and lazygit disagree on edge cases. With the CLI,
the user's configuration, credential helpers, SSH agent and `~/.ssh/config`,
worktrees, sparse checkouts, and newer Git features behave exactly as in the
user's shell and in lazygit. Git 2.34 or newer is required and checked at
startup.

The typed wrapper in `internal/git` owns argument construction, the working
directory, the child environment and process isolation, timeouts and
cancellation, output capture, and parsing into domain models. It does not own
status calculation, transports, authentication, merging, or credentials.

### 3.2 Commands

**Validation**, once per candidate at startup and on a full rebuild:

```text
git -C <path> rev-parse --path-format=absolute --git-dir --git-common-dir --show-toplevel \
    --git-path MERGE_HEAD --git-path rebase-merge --git-path rebase-apply \
    --git-path CHERRY_PICK_HEAD --git-path REVERT_HEAD --git-path BISECT_LOG \
    --git-path FETCH_HEAD
```

It runs only for paths with a `.git` entry. A path without one is classified
through the file system (bare repository if it has `HEAD`, `objects`, and
`refs`), because Git refuses `-C` access to bare repositories with
`safe.bareRepository = explicit`. The worktree root, Git directory, common
directory, and marker paths are cached. `--git-path` resolves per-worktree
files correctly; since `rev-parse` has no NUL mode, paths containing a
newline are rejected.

**Inspection**, three processes per entry:

```text
git --no-pager -C <root> status --porcelain=v2 --branch --show-stash -z --untracked-files=<normal|no>
git --no-pager -C <root> for-each-ref --format=<branch format> refs/heads [<upstream patterns>]
git --no-pager -C <root> config -z --get-regexp '^remote\..*\.url$'
```

with the branch format

```text
%(HEAD)%00%(refname)%00%(upstream:short)%00%(upstream)%00%(upstream:track,nobracket)%00%(worktreepath)%00%(objectname)%00%(objectname:short)%00%(authorname)%00%(committerdate:unix)%00%(subject)
```

- `status` supplies branch, HEAD, upstream, ahead/behind, all entries, and,
  since Git 2.35, the number of stash entries (`# stash N`, omitted for
  none).
- `for-each-ref` supplies every local branch with upstream, untranslated
  tracking state (empty, `gone`, `ahead N`, `behind N`, `ahead N, behind M`),
  the worktree that has it checked out, and its latest commit; the `*` line is
  the latest commit of HEAD. With `[branches].enabled = false` it is limited
  to the checked-out branch and skipped for an unborn or detached HEAD.
- The upstream patterns select the upstream that `status` names in short
  form (`refs/remotes/<upstream>`, `refs/<upstream>`, and, if only the
  checked-out branch is listed, `refs/heads/<upstream>`). The line whose full
  refname equals `%(upstream)` of the `*` line supplies the upstream commit,
  which the update applies (see 4.2), without a further process. If the
  branch or upstream changed between the two calls, or no line matches, the
  upstream commit stays unknown and the repository is not updated.
- Only a detached HEAD, or a branch switched between the two calls, needs a
  fourth process: `git log -1 --no-color --format=%H%x00%h%x00%an%x00%ct%x00%s`.
- Git 2.34 omits the stash header in porcelain v2, so it needs another
  process: `git rev-list --walk-reflogs --count --ignore-missing refs/stash`,
  which prints `0` if there is no stash. The version check at startup selects
  the method. Stash entries belong to the repository store, so all its
  worktrees report the same count. The dashboard only reports them; applying
  or dropping a stash stays with lazygit.
- `config` exits with 1 when no remote exists.
- Operations in progress and the last fetch time come from `stat` calls on
  the cached marker paths. The last fetch time of an entry is the newest
  `FETCH_HEAD` among all entries of its store, so fetches from lazygit or a
  shell count too.

**Recent commits**, only for the repository selected in the detail panel
and only with `[ui].detail_commits` above 1: `git log --max-count=<n>
--no-color --no-show-signature --format=%H%x00%h%x00%an%x00%ct%x00%s HEAD --`.
The result is cached per repository for the HEAD commit it was read at, so
it is read again only after HEAD moves or after a failure and a refresh. A
request for a previously selected repository is cancelled when the selection
moves on, so scrolling through the list runs at most one such process at a
time. It is not part of inspection, so the process budget of a refresh
stays unchanged.

**Fetch**, once per store: `git -C <root> fetch --all [--prune]`.

**Update**: `git -C <root> merge --ff-only --no-autostash --no-overwrite-ignore <upstream commit>` (see 4.2).

### 3.3 Parsing rules

- `# branch.oid (initial)` means unborn; `# branch.head (detached)` means
  detached; no `# branch.upstream` means no upstream; an upstream without
  `# branch.ab` means the upstream is gone. Unknown header lines are ignored.
- Entry types `1`, `2`, `u`, `?`, `!` are parsed. An unknown type makes the
  worktree state unknown; it is never reported as clean.
- Counting: staged = type `1`/`2` with index status ≠ `.`; modified =
  worktree status `M` or `T`; deleted = worktree status `D` (staged deletions
  count as staged); renamed = type `2`, in addition to staged; conflicts =
  type `u`; untracked = type `?`.
- Never parse localized or human-oriented output, colors, or column spacing;
  use NUL-delimited output and explicit formats. Git's error messages are not
  parsed; stderr is shown verbatim.

### 3.4 Process isolation

Every Git process started while the TUI owns the terminal:

- gets `GIT_TERMINAL_PROMPT=0`, `GIT_PAGER=cat`, `--no-pager`, and
  `GIT_EDITOR=true`; read-only commands also get `GIT_OPTIONAL_LOCKS=0`, so
  `status` never takes `index.lock` while lazygit or an editor works in the
  same repository;
- reads `/dev/null`; stdout and stderr are captured;
- starts in a new session (`setsid`) without a controlling terminal, so ssh
  and credential helpers cannot open `/dev/tty` and a prompt fails instead of
  writing into the TUI. ssh-agent, credential helpers, and `*_ASKPASS`
  programs keep working;
- leads its own process group. Timeouts and cancellation terminate the whole
  group, first with `SIGTERM` so Git can remove lock files, then with
  `SIGKILL` after 3 seconds. After Git exits, output pipes are drained for at
  most 2 seconds, so a lingering grandchild such as an ssh `ControlPersist`
  master cannot turn success into a timeout;
- runs without `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`,
  `GIT_COMMON_DIR`, `GIT_OBJECT_DIRECTORY`, and `GIT_NAMESPACE`, which would
  otherwise redirect every command to one repository. The same applies to
  lazygit and launchers. Everything else, including the locale, is inherited.

Text from external sources (commit subjects, names, URLs, stderr, directory
names) is displayed with control characters replaced, so it cannot inject
terminal escape sequences. headquarters never changes Git configuration,
including `safe.directory`.

### 3.5 Validation outcomes

| Outcome | Explicit entry | Discovered candidate |
|---|---|---|
| valid worktree root | available | included |
| path below the worktree root | `config error` naming the root | not applicable |
| bare repository | `unsupported: bare repository` | never a candidate |
| not a repository | `not a repository` | ignored |
| path does not exist | `missing` | not applicable |
| directory owned by another user | `untrusted owner`, hint to `safe.directory` | listed as `untrusted owner` |

## 4. Batch operations

Lowercase keys (`f`, `u`) act on the marked repositories if any are marked,
otherwise on the cursor row; uppercase keys (`F`, `U`) act on all visible
rows regardless of marks. Marks survive filter changes; the update
confirmation and the fetch message state how many marked targets are hidden.

### 4.1 Fetch

- One fetch per repository store, even if several of its worktrees are
  targeted; all entries of the store are inspected again afterward.
- Non-interactive (see 3.4): a fetch that needs a passphrase or host-key
  confirmation fails. It can be retried in the terminal through the same
  handover as lazygit (see 5), where the user answers the prompt.
- Bounded by `fetch_parallelism`, with a timeout per fetch. One failure never
  stops the batch; failed and cancelled fetches can be retried.
- Starts without a confirmation: a fetch changes only remote-tracking refs,
  never a local branch or the worktree.

### 4.2 Safe update

A repository is eligible only if every condition holds:

- no staged, modified, deleted, renamed, or conflicting entries (untracked
  and ignored files do not block; the update never overwrites them, see
  below);
- no merge, rebase, cherry-pick, revert, or bisect in progress;
- HEAD is a local branch with commits, with an upstream that still exists
  and whose commit is known;
- the branch is behind and not ahead of its upstream;
- no other mutating job and no interactive session is active for its store.

The update runs `merge --ff-only --no-autostash --no-overwrite-ignore
<upstream commit>` instead of `pull --ff-only`: the merge itself does not
contact the network (the fetch before it is a separate job, see below), and
it applies exactly the upstream commit that inspection read when the update
was planned, never a later state of `@{upstream}`; it is independent of
`pull.*` configuration; and `--no-autostash` guarantees that no stash is
created even with `merge.autoStash`. Git refuses to overwrite untracked files
on its own, but by default replaces ignored files, such as a local
configuration that the upstream starts to track; `--no-overwrite-ignore`
makes such a collision fail like any other, so the repository stays on the
worklist with Git's message and no file or branch is changed.

Every condition is checked again under the store lock immediately before the
merge; a changed repository is skipped with a reason. The fresh state must
also match the plan: the same branch, HEAD commit, upstream, and upstream
commit. A fetch by another tool after the plan, including a rewritten
upstream with the same distance, a branch switch, or a new local commit
therefore skips the repository with a request to try again instead of
applying something that was not planned. The store lock coordinates
only headquarters's own jobs; an external Git process between the recheck
and the merge is not excluded, but cannot change the commit that the merge
applies.
headquarters never creates a merge commit, a rebase, or a stash.

`u` and `U` open a confirmation that lists the targets with their state
before the fetch and describes what the update does in plain words instead
of Git commands. Confirming fetches every target store first. When the fetch
and the following inspections are finished, the update is planned with the
fetched state and starts on its own; the merge applies exactly the
upstream commit read after the fetch. The user confirms the intent ("fetch,
then fast-forward what is safe"), not a list of commits, because that list
is only known after the fetch. A failed fetch becomes the skip reason of
every target of its store, even if the old state looked eligible, so the
repository stays on the worklist and is not updated with stale state. A
cancelled fetch cancels the update.

**Rule: asynchronous completion never opens a confirmable dialog.** A dialog
that opens on its own lets a key meant for the previous view confirm an
action. Finished background work reports through a message and waits for an
explicit key. The update that follows its fetch was already confirmed and
opens nothing: it shows in the operations view only if that view is open.

### 4.3 Worklist

After an update, the operations view is the worklist of the batch: updated,
failed, and cancelled repositories plus those the plan skipped (except
those already up to date). Rows can be opened in lazygit; after the next
inspection they show `→ now eligible`, `→ now up to date`, or
`→ still blocked: <reason>`. `u` opens a new confirmation for everything not
yet updated. Resolving a blocker remains work done in lazygit.

### 4.4 Other local branches

Rows always describe the checked-out branch. Other local branches are
evaluated from the `for-each-ref` call that inspection runs anyway, and only
those that need attention are reported: no upstream, upstream gone, ahead, or
behind. Reporting every branch would be too noisy, and a fixed base branch
per repository would miss unpublished work, so the decision combines both:

- all local branches, filtered by `[branches].max_age` and `ignore`;
- pinned branches (`main`, `master` by default) bypass the filters; a
  missing pinned branch is reported only for per-repository lists, so the
  global default produces no warnings everywhere;
- branches checked out in another worktree of the store are never reported.

Constraints: only `refs/heads`; the row state, filters `1`–`5`, sorting, and
the safe update keep referring to HEAD. A merged branch without upstream is
reported until it exceeds `max_age`, because detecting merged branches would
need another process or Git 2.41. Comparing with a base branch and
remote-only branches are out of scope. Fast-forwarding a non-checked-out
branch (`git fetch . refs/remotes/<remote>/<b>:refs/heads/<b>`) would be a
separate, opt-in operation with its own preview and must never be folded
into `u`.

### 4.5 Launchers

Launchers are argument arrays without a shell. They start in the worktree
root, placeholders (`{path}`, `{workspace}`) must be complete arguments, and
they take over the terminal like lazygit. *Open remote in browser* derives an
HTTPS URL from the primary remote (configured `remote_name`, else `origin`,
else the alphabetically first remote); for SSH remotes it assumes the web
interface uses the same host and path.

## 5. Terminal handover

lazygit, the interactive fetch retry, and launchers run as synchronous
foreground child processes through Bubble Tea's `tea.ExecProcess`: working
directory is the worktree root, stdin, stdout, and stderr are the terminal,
and the environment is the user's without the redirecting `GIT_*` variables.
headquarters emits no terminal escape sequences of its own. Interactive
children are never bound to a cancellable context; cancelling dashboard work
must not kill an interactive session.

While a child owns the terminal:

- headquarters does not render or read keys;
- running batch jobs continue; they cannot touch the terminal (see 3.4);
- workers never block on the user interface. They report to the scheduler,
  which buffers events in an unbounded queue; one forwarder delivers them to
  Bubble Tea after the handover, because Bubble Tea processes no messages
  while a child runs;
- no new job starts for the open store; opening a store with an active
  mutating job is refused.

Afterward, the terminal mode and size are restored and all entries of the
store are inspected again, regardless of the exit status, since changes may
have happened before a failure. Filter and selection are kept. If terminal
restoration fails, headquarters exits with a fatal error instead of running
with a corrupted screen.

lazygit runs as a separate process instead of being embedded because its
internal packages are not a public API; the process boundary keeps upgrades
independent, uses the user's own lazygit version and configuration, and
makes terminal ownership unambiguous. A missing lazygit is detected at
startup and reported on `Enter`; the dashboard stays usable.

## 6. Structure

### 6.1 Packages

| Package | Responsibility |
|---|---|
| `cmd/headquarters` | flags, configuration lookup, version check, `--check` |
| `internal/config` | strict TOML decoding, defaults, path expansion, validation, theme presets |
| `internal/inventory` | discovery, patterns, identity, precedence, validation states |
| `internal/git` | isolated command runner, parsers, inspection, fetch, update |
| `internal/jobs` | scheduler with pools, timeouts, store locks, event queue |
| `internal/app` | application services: inspection, fetch and update jobs, eligibility, filters, sorting |
| `internal/launcher` | lazygit, custom launchers, browser |
| `internal/domain` | repository and operation models, without dependencies on UI, processes, or Git formats |
| `internal/tui` | Bubble Tea model, views, layout, theme, icons, key map |
| `internal/testutil` | real Git repositories for tests |

The TUI consumes application services and domain models and never builds Git
commands. The domain depends on nothing else in the project.

### 6.2 Domain model rules

- `Known[T]` distinguishes "not determined" from a valid zero value. A failed
  inspection invalidates the state; it never yields `Behind = 0` or a clean
  worktree.
- Derived values such as `Clean()` and `Diverged()` are computed, not stored,
  so they cannot contradict their inputs.
- Every inspection carries a generation; an older result never overwrites a
  newer one.

## 7. Concurrency and consistency

- Inspections run concurrently (`inspection_parallelism`, with a timeout
  each) and are applied incrementally; the table is usable before they
  finish.
- Locks are keyed by repository store, not by worktree. At most one mutating
  operation runs per store; fetches of different stores run concurrently
  within `fetch_parallelism`.
- An interactive session locks its store against new dashboard operations.
- Cancelling a batch prevents new jobs and terminates running process groups.
- After an operation or a handover, all entries of the affected stores are
  inspected again. A full inventory rebuild (`R`) is separate because it may
  add or remove discovered repositories.

## 8. Errors and security

- Errors are kept per repository and name operation, repository, and cause.
  One broken repository never makes the dashboard unusable, and errors are
  never replaced by plausible values. Stderr of failed children is kept,
  bounded.
- Nothing is written to the terminal outside the TUI. User information in
  URLs (`https://user:token@host/…`) is redacted before display.
- No shell is ever involved; paths and URLs are data. Configured executables
  are resolved at startup and must be regular executable files.
- Operations that change local branches require a confirmation that lists
  their targets, and their preconditions are checked again immediately
  before execution.
- No automatic stash, merge, rebase, reset, force-push, clone, remote
  rewrite, or Git configuration change.

## 9. User interface principles

The concrete keys, symbols, and filters are listed in
[keybindings.md](keybindings.md).

- lazygit-style panels: rounded borders with the title in the top border and
  secondary information in the bottom border. Table and detail panel sit side
  by side from 130 columns, otherwise stacked; beside the table, the detail
  panel takes 40 % of the width and at most 70 columns, so the table gets
  the space it needs first. If the table needs less than the rest, the
  repository panel is only as wide as the table and the detail panel moves
  left to it and takes the remaining width, so no empty band separates the
  two. The selected row is highlighted only to the right edge of the table,
  like the table header. Dialogs are centered over a dimmed copy of
  the main view.
- **Color is never the only carrier of information.** Every state has a
  symbol with a count or text, and the help view has a legend. Colors apply
  per cell, so problems stand out and healthy rows stay quiet; success has
  its own color so it is not confused with commits ahead. `NO_COLOR` is
  honored.
- A health symbol in its own column at the left edge shows the most
  important state of the row, so all names start in one column, by priority: errors, critical states (conflicts, operations in
  progress, divergence), dirty, behind, ahead, other attention (other
  branches, stash entries, a remote mismatch, a detached HEAD, or a gone
  upstream). Only healthy names are not emphasized. Family rows summarize
  the health of their repositories.
- The footer shows only the actions that apply to the current mode, row, and
  selection; `?` shows all keys.
- The table header is a bar in its own colors, so the columns stand apart
  from the rows. Header counters with matches are boxes in their state
  color, so problems are visible at a glance; the active quick filter is
  bracketed, because reverse video and colors are lost with `NO_COLOR`.
- The detail panel puts branch, worktree, and errors first, because a small
  panel is cut off at the bottom; it shortens the other branches list before
  dropping section titles. Each section starts with its title in a rule
  across the panel (`── BRANCH ───`), so sections stay apart without boxes.
  Each recent commit is one line of short hash, time, and subject; when the
  panel is small, the older commits are dropped first, oldest first. lazygit
  shows the full history.
- Commit times have one absolute format in table and detail panel
  (`Nov 13 22:16`, with the year for an earlier year), so rows can be
  compared and sorted by eye; a narrow column drops the time of day and then
  the day instead of truncating. Fetch and branch ages stay relative,
  because there the distance to now is what matters.
- Every count has its own column (staged, modified, untracked, stashed,
  ahead, behind), so the same state can be compared down the table, and a
  cell needs no symbol. Local work comes first, the relation to the
  upstream last. A known zero is `–`, an unknown value `?`, and a count
  that does not apply stays empty, so neither looks like a clean state.
  Findings without a count, such as conflicts, an operation in progress, or
  a missing upstream, share one State column with text, which is empty for
  a repository without findings.
- Column widths follow the content of all visible rows, so the table does not
  shift while scrolling; the State column reserves room for transient states
  and shrinks last. If the full titles of the count columns do not fit, they
  are replaced by their symbols instead of squeezing names and branches.
- The operations view groups rows by status; when it scrolls, the title of
  the first shown group stays visible, because rows carry their status only
  through their group.
- Unavailable entries keep their sort position and show their state in the
  table.

## 10. Non-functional requirements

- **Platforms:** Linux and macOS. Code avoids gratuitous platform coupling,
  but Windows is not supported.
- **Git:** 2.34 or newer.
- **Performance:** 200 repositories on a local SSD are inspected within
  2 seconds of a cold start, excluding the network.
- **Process budget:** three Git processes per entry and refresh (four for a
  detached HEAD, one more with Git 2.34); one validation process per
  candidate at startup and on a rebuild; one process for the recent commits
  of the selected repository when its HEAD changes.
- **Terminal integrity:** no background process can read from or write to
  the terminal.
- **Robustness:** a failing, hanging, or slow repository never blocks the
  user interface or other repositories.

## 11. Testing

- **Unit tests** cover configuration, precedence, patterns, identity,
  parsers (including unknown header lines and entry types), eligibility,
  fetch deduplication, environment sanitization, scheduler limits, and the
  TUI update logic. TUI tests drive the model with key messages.
- **Integration tests** use real temporary repositories for every state the
  dashboard shows: dirty and clean worktrees, conflicts and operations in
  progress, detached and unborn HEAD, upstream states, remotes, linked
  worktrees, bare and invalid repositories, inventory precedence, symlink
  loops, case-insensitive deduplication on macOS, no `index.lock` during
  inspection, a fetch whose ssh tries to read `/dev/tty` failing promptly,
  process-group termination, and fast-forward acceptance and rejection.
  Expected states come from the Git CLI itself.
- **Handover tests** use a controlled helper executable instead of lazygit
  that reports its working directory and environment and returns chosen exit
  codes; they also verify that events produced during a handover arrive
  afterward. The real lazygit handover is tested manually: alternate screen,
  input, resize, redraw, refresh.
- **Performance** is guarded by deterministic tests: a counting wrapper
  around the real Git verifies the process budget of validation and
  inspection (see 10), and a scheduler test verifies that jobs actually run
  in parallel up to their limit. These are the two properties that determine
  startup time, and they do not depend on the speed of the machine.
- **Benchmark** (`make perf`, local only): 200 generated repositories must be
  inspected within 2 seconds. It is not part of CI because starting processes
  on shared runners, especially macOS, is slow and erratic enough to miss the
  target without any change in the code.
- **CI** runs lint and tests with the race detector and coverage on Linux
  with Git 2.34 (the minimum), Linux with the latest Git, and macOS. A
  version-tag push creates a release draft only after this matrix passes;
  publication remains a manual decision.

## 12. Decisions

| Decision | Reason |
|---|---|
| Go with Bubble Tea and Lip Gloss | mature TUI ecosystem, simple bounded concurrency with `context`, direct process control, single static binary. Rust was technically excellent but more costly for this scope; Python and Node.js complicate distribution. |
| Official Git CLI only | see 3.1 |
| TOML configuration | explicit types, no implicit conversions, comments, repeated tables map to repositories; YAML's extra features are not needed. Unknown fields are rejected before 1.0 to catch typos. |
| lazygit as a child process | see 5 |
| Hybrid inventory, explicit entries win | see 2.1 |
| Local path as identity, remote as assertion | see 2.4 |
| Families explicit, never derived from directories | predictable grouping |
| Linked worktrees as rows, grouped into stores | separate state per worktree, shared locks and fetches per store |
| Last fetch time from `FETCH_HEAD` | counts fetches from any tool |
| `merge --ff-only --no-autostash --no-overwrite-ignore <upstream commit>` | see 4.2 |
| No dialog opens on asynchronous completion | see 4.2 |
| Other branches reported only when they need attention | see 4.4 |

Possible later features, none planned: profiles, saved filters, tags,
favorites, an explicit clone action for declared but missing repositories,
machine-readable status export, background refresh, and fast-forwarding of
non-checked-out branches.
