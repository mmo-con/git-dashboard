# Git Dashboard — Project Plan and Design Document

## 1. Executive Summary

### Product name

**Git Dashboard**

### Purpose

Git Dashboard is a modern terminal application for managing a curated collection of local Git repositories. It provides a cross-repository overview, groups repositories into families, supports safe batch operations such as fetch, and hands off all detailed work within one repository to **lazygit**.

Git Dashboard does not replace lazygit. It adds the multi-repository perspective that a single-repository tool intentionally does not provide.

### Guiding principle

> Git Dashboard manages the repository collection. lazygit manages the individual repository.

### Primary goals

- Maintain an intentional inventory of repositories.
- Optionally discover repositories below configured root directories.
- Show the most important state of all included repositories.
- Group, filter, and sort repositories.
- Run safe operations across the currently visible repositories.
- Open a selected repository in lazygit without breaking the terminal session.
- Refresh repository state after returning from lazygit.

### Non-goals

Git Dashboard will not:

- implement a complete Git user interface,
- create or edit commits,
- implement merge, rebase, stash, or conflict-resolution workflows,
- implement its own Git protocol, credential, or SSH stack,
- embed or fork lazygit,
- perform implicit merges, rebases, resets, or force pushes,
- manage Windows as a supported platform.

---

## 2. Problem Statement

Developers often keep many Git repositories below one or more development directories. Only a subset of the directories may be Git repositories, and only a subset of those repositories may be relevant to a particular dashboard.

The repository layout may also be heterogeneous:

```text
/path/to/devrepos/
├── dotfiles/                    # repository
├── notes/                       # not a repository
├── company/
│   ├── api/                     # repository, nested one level deeper
│   ├── frontend/                # repository, nested one level deeper
│   └── documentation/           # ordinary directory
├── experiments/
│   ├── parser/                  # repository, excluded from dashboard
│   └── compiler/                # repository
└── archive/
    └── legacy-service/          # repository, intentionally excluded
```

Single-repository tools such as lazygit are excellent once a repository has been selected, but they do not answer cross-repository questions efficiently:

- Which relevant repositories contain uncommitted changes?
- Which repositories are behind their upstream?
- Which fetch operations failed?
- Which projects have not been touched recently?
- Which repositories belong to the same family?
- Which visible repositories can safely be fast-forwarded?

Git Dashboard provides that overview while reusing the official Git implementation and lazygit for the operations they already solve well.

### 2.1 Related tools

Several existing tools cover parts of the multi-repository problem:

| Tool | Focus | Gap relative to Git Dashboard |
|---|---|---|
| gita | CLI that shows the status of registered repositories side by side, supports groups, and runs Git commands across them | line-oriented command output instead of an interactive full-screen view; no handover to a single-repository TUI |
| myrepos (`mr`) | configuration-driven execution of commands across many repositories and version-control systems | command runner; no persistent status overview, filtering, or selection |
| mu-repo | runs Git commands across a set of repositories | command runner without an interactive overview |
| ghq | clones repositories into a structured directory layout and lists them, typically combined with fzf | inventory and navigation only; no status, batch fetch, or safety checks |
| lazygit | complete single-repository TUI, including a recent-repositories switcher | intentionally single-repository; no cross-repository status |

Git Dashboard's distinguishing combination is:

- a curated inventory with bounded discovery;
- an interactive cross-repository status table with filters and families;
- safe batch operations with explicit previews;
- a seamless handover to lazygit.

Git Dashboard does not aim to become a general command runner across repositories.

---

## 3. Repository Inventory Model

### 3.1 Design decision

Git Dashboard uses a **hybrid inventory**:

1. repositories may be listed explicitly by local path;
2. repositories may be discovered below configured roots;
3. include and exclude rules determine which discovered repositories enter the dashboard.

This resolves all of the following cases:

- only some subdirectories are Git repositories,
- some valid Git repositories must not appear,
- repositories are nested at different depths,
- a user wants exact control over important repositories,
- a user does not want to maintain a long list manually.

Explicit repositories and discovered repositories are normalized and deduplicated by canonical local path.

### 3.2 Why not scan every directory unconditionally?

An unrestricted recursive scan has undesirable properties:

- it may traverse large dependency or build trees;
- it may find repositories that are unrelated to the dashboard;
- its behavior changes merely because another repository was cloned somewhere below the root;
- symlinks can cause duplicates or traversal loops;
- startup cost becomes difficult to predict.

Discovery therefore has explicit depth and exclusion limits.

### 3.3 Explicit repositories

Explicit local paths are the authoritative mechanism for repositories that must always be present:

```toml
[[repositories]]
path = "/path/to/devrepos/dotfiles"
family = "Personal"

[[repositories]]
path = "/path/to/devrepos/company/api"
family = "Work"

[[repositories]]
path = "/path/to/devrepos/company/frontend"
family = "Work"
```

Advantages:

- deterministic inventory,
- no dependency on directory depth,
- straightforward family assignment,
- repository-specific settings can be added later,
- missing checkouts can be reported precisely.

### 3.4 Discovered repositories

Discovery is useful for broader or less stable areas:

```toml
[[discovery]]
root = "/path/to/devrepos/experiments"
max_depth = 3
family = "Experiments"
include = ["**"]
exclude = [
  "archive/**",
  "**/vendor/**",
  "**/node_modules/**"
]
follow_symlinks = false
```

#### Traversal rules

- Discovery walks directories below `root` up to `max_depth` levels. The root itself has depth 0.
- A directory is a candidate if it contains a `.git` directory or a `.git` file. Every candidate is validated through Git (see 7.6).
- Non-repository directories are ignored; they are not errors.
- Once a repository is found, the walk does not descend into it. Repositories nested inside another repository, including submodules and vendored checkouts, are never discovered. They enter the dashboard only through explicit `[[repositories]]` entries.
- Bare repositories have no `.git` entry and are therefore never discovered.
- Symbolic links are not followed unless `follow_symlinks = true`. When they are followed, directories that were already visited (identified by device and inode) are skipped to prevent loops and duplicates.

#### Pattern semantics

- Patterns use `doublestar` glob syntax: `*`, `**`, `?`, `[...]`, and `{a,b}`.
- Patterns are matched against the slash-separated directory path relative to the discovery root, for example `company/api`.
- A pattern ending in `/**` also matches the directory itself.
- `include` defaults to `["**"]`.
- A directory that matches an `exclude` pattern is pruned: neither it nor anything below it is traversed. Large trees such as `node_modules` therefore do not affect startup time.
- A candidate is added only if its relative path matches at least one `include` pattern and no `exclude` pattern.
- Matching is case-sensitive on all platforms.

#### Families of discovered repositories

Every repository found by a discovery entry receives that entry's `family`. If the discovery entry has no `family`, the repository belongs to the built-in family `Ungrouped`. Family names are never derived from directory names.

### 3.5 Inclusion precedence

The inventory is computed in three steps:

1. **Discover** candidates below every `[[discovery]]` root, applying that root's include and exclude patterns.
2. **Merge** discovered candidates with explicit `[[repositories]]` entries by repository identity (see 3.6).
3. **Resolve** every identity with the following table.

| Explicit entry | Found by discovery | Result |
|---|---|---|
| none | yes | included with the discovery entry's settings |
| `enabled = true` (default) | no, excluded, or outside every root | included with the explicit settings |
| `enabled = true` (default) | yes | included once; explicit fields override discovery fields |
| `enabled = false` | any | excluded |
| none | no | not part of the inventory |

Consequences:

- An explicit entry always wins over discovery exclusions. No additional flag is required.
- `enabled = false` is the mechanism for suppressing a single repository that a discovery rule would otherwise include.
- If several discovery roots find the same repository, the first root in configuration order supplies the defaults and a configuration warning is reported.

Example:

```toml
[[repositories]]
path = "/path/to/devrepos/archive/important-tool"
family = "Tools"
enabled = true

[[repositories]]
path = "/path/to/devrepos/experiments/parser"
enabled = false
```

The first repository is included although it may sit below a broadly excluded directory. The second repository is excluded although a discovery rule would include it.

### 3.6 Repository identity

- The identity of an entry is its canonical worktree root: an absolute path after `~` and environment expansion (see 12.3), with symbolic links resolved through `filepath.EvalSymlinks`.
- Deduplication additionally compares directories with `os.SameFile` (device and inode). This handles case-insensitive file systems on macOS and aliases such as `/var` and `/private/var`.
- An explicit `path` must be the worktree root reported by `git rev-parse --show-toplevel`. If it points to a subdirectory of a repository, the entry is shown with the status `config error`, and the message names the actual worktree root. The path is never normalized silently.
- Linked worktrees are separate entries with their own rows. Entries that share the same common Git directory (`git rev-parse --git-common-dir`) belong to the same **repository store**. The detail pane lists the other worktrees of the same store. Network operations and locks act per store (see 14).
- Bare repositories are not supported as dashboard entries. An explicit entry that points to a bare repository is shown as `unsupported: bare repository`.

### 3.7 Local paths versus repository URLs

#### MVP decision

The local directory is the primary identity and required configuration value. Git Dashboard expects the repository to be checked out already.

```toml
[[repositories]]
path = "/path/to/devrepos/company/api"
```

This matches the initial product responsibility: managing existing local repositories.

#### Optional expected remote

A remote URL may be configured as an optional assertion:

```toml
[[repositories]]
path = "/path/to/devrepos/company/api"
remote = "git@github.com:example/api.git"
remote_name = "origin"
```

If present, Git Dashboard can compare it with the configured Git remote and report a mismatch. It must not silently rewrite the remote.

Remote URLs are optional because:

- local repositories may have multiple remotes;
- SSH and HTTPS URLs can refer to the same repository;
- forks often use `origin` and `upstream` differently;
- a moved directory is still best identified by its local path in a local dashboard;
- URLs may contain environment-specific host aliases.

URL normalization should initially be conservative. A string mismatch should be reported as a warning rather than treated as proof that the repositories differ.

#### Future bootstrap capability

A later release may support declarative repositories that are not checked out:

```toml
[[repositories]]
path = "/path/to/devrepos/company/api"
remote = "git@github.com:example/api.git"
clone_if_missing = false
```

Possible states would then include:

- available,
- missing checkout,
- path occupied by a non-repository,
- remote mismatch.

Automatic cloning is not part of the MVP. If added, it must be an explicit user action and use the official Git CLI.

---

## 4. Users and Core Use Cases

### Primary user

A developer who manages many local Git repositories and already works with terminal tools.

### Core use cases

1. View an intentional collection of repositories.
2. Discover repositories below selected roots without including everything.
3. See branch, remote, latest commit, and worktree status.
4. Filter by family, name, path, or status.
5. Open one repository directly in lazygit.
6. Fetch all currently visible repositories.
7. Review failures and retry selected operations.
8. Identify repositories behind their locally known upstream.
9. Optionally verify that a local checkout has the expected remote.

---

## 5. Functional Scope

### 5.1 MVP

#### Configuration and inventory

- Load a TOML configuration file.
- Support explicit repository paths.
- Support bounded repository discovery.
- Support include and exclude patterns.
- Support repository families.
- Support disabled repository entries.
- Deduplicate repositories by canonical local path.
- Validate configuration and report actionable errors.
- Report configured repositories that are missing or invalid.

#### Repository metadata

For every included repository, display:

- display name,
- local path,
- family,
- linked worktrees of the same repository store,
- current branch, detached HEAD, or unborn branch without commits,
- operation in progress: merge, rebase, cherry-pick, revert, or bisect,
- clean or dirty status,
- counts for staged, modified, deleted, renamed, conflicting, and untracked entries; `untracked not checked` when untracked detection is disabled for the repository,
- configured remotes and their URLs,
- primary remote: the configured `remote_name`, otherwise `origin`, otherwise the alphabetically first remote,
- expected-remote mismatch,
- latest commit:
  - abbreviated object ID,
  - subject,
  - author,
  - timestamp,
- upstream state:
  - up to date,
  - ahead,
  - behind,
  - diverged (ahead and behind),
  - no upstream,
  - upstream gone,
- time of the last fetch, derived from the modification time of `FETCH_HEAD`, so fetches from lazygit, a shell, or Git Dashboard are all counted,
- inspection or operation errors.

#### Terminal UI

- Repository table.
- Detail pane for the selected repository.
- Keyboard navigation.
- Text filter by name and path.
- Filter by family and repository state (`is:` tokens in the text filter, see 13.2).
- Sort by:
  - name,
  - family,
  - latest commit time,
  - worktree state,
  - upstream state.
- Multi-selection for batch actions: toggle, mark all visible, clear marks.
- Manual refresh.
- Loading, empty, missing, and error states.
- Help view with key bindings.

#### lazygit integration

- Open the selected repository in lazygit with `Enter`.
- Pause Git Dashboard while lazygit owns the terminal.
- Restore Git Dashboard after lazygit exits.
- Reinspect the selected repository after returning.
- Preserve the current filter and selection where possible.
- Show a useful error if lazygit is unavailable or exits unsuccessfully.

#### Batch fetch

- `F` fetches all visible repositories.
- `f` fetches the marked repositories if any are marked; otherwise it fetches the repository under the cursor.
- Use the official Git CLI once per repository store:

  ```text
  git -C <worktree-root> fetch --all [--prune]
  ```

  `--prune` is added when `git.fetch_prune = true`, which is the default.
- If several targeted entries belong to the same repository store, the store is fetched once and all of its entries are reinspected afterward.
- Git runs non-interactively (see 7.5). A fetch that needs a passphrase, a password, or a host-key confirmation fails instead of prompting.
- A failed fetch can be retried in the terminal: Git Dashboard hands the terminal to `git fetch` exactly as it does for lazygit, the user answers any prompt, and the repository is reinspected afterward.
- Limit concurrent fetch operations with `git.fetch_parallelism`.
- Apply a timeout per fetch (`git.fetch_timeout`, default 120 seconds). On timeout or cancellation, the complete process group is terminated.
- Show progress per repository.
- Do not stop the entire batch when one repository fails.
- Allow failed operations to be retried.
- Allow the batch to be cancelled.
- Reinspect affected repositories after completion.

The term **visible** means the repository rows remaining after the active family, text, and status filters have been applied. The confirmation or progress view must show the final target count before work starts.

### 5.2 After the MVP

The following features are delivered in phases 4 and 5 (see 18). The reporting of local branches other than HEAD was added afterward.

#### Safe batch update

`u` opens the preview of an `Update eligible` operation. It targets the marked repositories if any are marked; otherwise it targets all visible repositories. A repository is eligible only if every condition holds:

- the worktree has no staged, modified, deleted, renamed, or conflicting entries; untracked files do not block the update because Git refuses to overwrite them;
- no merge, rebase, cherry-pick, revert, or bisect is in progress;
- HEAD points to a local branch that already has commits;
- the branch has an upstream, and the upstream still exists;
- the branch is behind its upstream and not ahead of it;
- no other mutating job runs for the repository store, and no lazygit session is open for it.

Execution:

```text
git -C <worktree-root> merge --ff-only --no-autostash @{upstream}
```

The update deliberately uses `merge --ff-only` against the already fetched upstream instead of `pull --ff-only`:

- it applies exactly the state shown in the preview, because it does not contact the network;
- it is independent of `pull.rebase`, `pull.ff`, and other pull configuration;
- `--no-autostash` guarantees that no stash is created even if `merge.autoStash` is configured.

Users fetch first with `F`. The preview shows the age of the last fetch for every repository.

Before execution, Git Dashboard shows:

- repositories to be updated;
- repositories to be skipped;
- the reason for each skip.

Immediately before each merge, all eligibility conditions are checked again. A repository whose state has changed is skipped with a reason.

Git Dashboard never creates a merge commit or a rebase.

#### Working through blocked repositories

After an update, the operations view (`b`) is the worklist of the batch. It lists the updated, failed, and cancelled repositories and also the repositories that the preview skipped; repositories that were already up to date are omitted because they need no attention.

- `Enter` opens the repository of the selected row in lazygit. The operations view stays open, and the repository is inspected again after lazygit exits.
- Once a row's repository has been inspected after the batch, the row shows `→ now eligible` or `→ now up to date`. A row whose repository was opened from the view shows `→ still blocked: <reason>` after the following inspection if the blocker remains.
- `u` opens a new safe-update preview for every repository of the batch that was not updated. The preview evaluates the current state again; nothing runs until it is confirmed, and confirming it replaces the worklist with the new batch.
- If the preview has no eligible repositories but some blocked ones, confirming it creates a worklist without starting a job.

The dashboard only tracks what is still open. Resolving a blocker, such as committing or stashing local changes, remains work done in lazygit.

#### Additional launchers

`o` opens a launcher menu for the selected repository. Launchers are configured explicitly:

```toml
[[launchers]]
name = "Shell"
command = ["zsh"]

[[launchers]]
name = "Editor"
command = ["nvim", "."]

[[launchers]]
name = "File manager"
command = ["yazi", "{path}"]
```

Rules:

- Commands are argument arrays. No shell is involved.
- The working directory is the worktree root.
- Placeholders must be complete arguments. `{path}` is replaced by the worktree root, `{workspace}` by the configured `workspace` of the repository or discovery entry (relative paths are resolved against the repository), or by the worktree root if none is configured.
- Launchers take over the terminal through the same handover as lazygit, and the repository is reinspected afterward.
- The built-in action `Open remote in browser` converts the primary remote URL to an HTTPS URL and opens it with `xdg-open` on Linux or `open` on macOS. For SSH remotes, it assumes the web interface uses the same host and path; it drops the SSH port and does not resolve SSH host aliases. If no HTTPS URL can be derived, the action is unavailable.

#### Local branches other than HEAD

**Status:** decided and implemented. Options A and B (see the table below) are combined; B is extended from a single base branch to a list of pinned branches.

Every row reflects the checked-out branch, because the row state relies on `git status --branch`. "Considering more branches" can mean three different things:

1. **Stale local branches:** `main` is behind its upstream while `feature/x` is checked out.
2. **Unpublished work:** a local branch is ahead of its upstream, has no upstream, or its upstream is gone. This is often the more important, safety-related question.
3. **Distance to a base branch:** how far the current branch is from `main`. This compares against a chosen base branch rather than the upstream, and it is a separate feature.

Questions 1 and 2 are answered for all local branches by the `for-each-ref` call of the inspection (see 7.4), which also supplies the latest commit of the checked-out branch and therefore replaces `git log -1`.

Options considered:

| Option | Configuration | Strength | Weakness |
|---|---|---|---|
| A: all local branches automatically | none | covers questions 1 and 2 without upkeep | very noisy in repositories with dozens of old branches |
| B: one additional branch per repository (`base = "main"`) | per repository | simple | too rigid (`main` and `develop`, release branches); misses unpublished work |
| C: list of branches per repository (`branches = [...]`) | per repository and/or global | focused, predictable | forgotten branches with unpublished work stay invisible |
| D: comparison with a base branch | per repository | answers question 3 | different feature; one extra process per compared branch |
| E: remote-only branches | – | – | belongs in lazygit, not in the dashboard |

Decision:

- **All local branches (A), reported only when they need attention:** a branch is reported if it has no upstream, its upstream is gone, or it is ahead of or behind its upstream. Up-to-date branches are never listed.
- **Noise containment for A:**
  - branches whose latest commit (`%(committerdate)`) is older than `[branches].max_age` (default `30d`; `"0"` disables the filter) are not reported;
  - branches matching a doublestar pattern in `[branches].ignore`, for example `"archive/**"`, are not reported.
- **Pinned branches (B, extended to a list):** `[branches].pinned` (default `["main", "master"]`) lists branches that are always evaluated, regardless of `max_age` and `ignore`. A repository entry may replace the list with `pinned_branches`. A pinned branch that does not exist is reported as `missing` only for per-repository lists, so that the global default does not produce warnings everywhere.
- **Display:** the upstream column shows only a count such as `⎇2`, in the warning color if at least one reported branch holds unpublished work (no upstream, upstream gone, ahead, or missing) and in the behind color otherwise. The detail panel lists the reported branches, pinned branches first in configuration order and the others by latest commit, newest first, truncated after 10 entries with "+N more".
- **Filters:** quick filter `6` and the token `is:branches` select repositories with reported branches; `is:unpublished` selects those with unpublished work.
- **Worktrees:** branches checked out in any worktree of the store are never reported; the worktree of the entry shows its own branch in its row.
- `[branches].enabled = false` restricts `for-each-ref` to the checked-out branch.

Constraints:

- Only local branches (`refs/heads`). No remote-only branches and no checkout from the dashboard; both conflict with the non-goals.
- Row state, the counters and state filters `1`–`5`, sorting, and the safe update keep referring to HEAD.
- Process budget: see 5.4 and 7.4.
- Known limitation: a branch without upstream that is already merged into its base keeps being reported until it is older than `max_age`. Detecting merged branches would need `for-each-ref --merged`, an additional process, or `%(ahead-behind:…)`, which requires Git 2.41.
- Not included: question 3 (option D) and remote-only branches (option E).
- Updating branches other than HEAD is a separate, opt-in decision. A non-checked-out branch can be fast-forwarded without touching the worktree by running `git fetch . refs/remotes/<remote>/<branch>:refs/heads/<branch>`. Without a leading `+`, Git rejects non-fast-forward updates and updates of branches checked out in any worktree. Because this changes local branches, it requires the preview and confirmation that apply to all batch operations that change local branches, and it must not be folded silently into `u`.

### 5.3 Possible later features

- Multiple named dashboards or profiles.
- Saved filter views.
- Tags in addition to families.
- Favorites or pinned repositories.
- Missing-checkout detection from declarative entries.
- Explicit clone action.
- Optional hosting-provider metadata.
- Machine-readable status export.
- Notifications for repositories not fetched recently.
- Optional background refresh.

These features are outside the MVP.

### 5.4 Non-functional requirements

- **Platforms:** Linux and macOS.
- **Git:** version 2.34 or newer. The version is checked at startup; an older Git produces a clear error message.
- **Performance:** with 200 repositories on a local SSD, inspection of all repositories completes within 2 seconds of a cold application start. Network operations are excluded. The table is interactive before inspection completes, and rows are filled in incrementally.
- **Process budget:** a refresh starts three Git processes per entry; only a detached HEAD needs a fourth one (see 7.4). Inventory validation starts one Git process per candidate and runs only at startup and on a full inventory rebuild.
- **Terminal integrity:** no Git process started by Git Dashboard can read from or write to the terminal while the TUI owns it.
- **Robustness:** a failing, hanging, or slow repository never blocks the user interface or other repositories.

---

## 6. Programming Language Evaluation

### 6.1 Evaluation criteria

The implementation language should provide:

- a strong TUI ecosystem;
- reliable process and terminal control;
- straightforward concurrency for repository inspection and batch jobs;
- simple distribution;
- fast startup and reasonable resource usage;
- good testability;
- long-term maintainability;
- first-class Linux and macOS support.

### 6.2 Go

#### Strengths

- Bubble Tea, Bubbles, and Lip Gloss provide an established TUI ecosystem.
- Goroutines, channels, and `context.Context` fit concurrent scans and cancellable jobs.
- `os/exec` provides direct and controlled child-process execution.
- Distribution as a single binary is straightforward.
- Startup is fast and resource usage is moderate.
- Linux and macOS builds are simple.
- Explicit error handling fits per-repository error reporting.
- The official Git CLI can be wrapped without shell interpolation.
- lazygit is also implemented in Go, which makes its process and terminal conventions easier to study if necessary.

#### Weaknesses

- TUI state requires disciplined model and message design.
- Layout and interaction code can become verbose.
- The codebase needs clear boundaries to avoid mixing UI state with process execution.

#### Assessment

**Recommended.**

Go offers the best balance of TUI libraries, concurrency, process control, maintainability, and simple distribution for this project.

### 6.3 Rust

#### Strengths

- Excellent performance and low resource usage.
- Strong type and memory safety.
- Good TUI libraries, notably Ratatui and Crossterm.
- Fine-grained control over processes and terminal state.

#### Weaknesses

- Higher implementation and onboarding cost.
- Concurrent TUI and process orchestration tends to require more code.
- The project does not need the additional low-level control enough to justify that cost.

#### Assessment

**Technically excellent, but unnecessarily expensive for the initial scope.**

Rust remains a valid choice if the primary developer strongly prefers it or if the application later grows into a substantially larger systems tool.

### 6.4 Python

#### Strengths

- Fast development.
- Strong libraries such as Textual and Rich.
- Straightforward subprocess integration.
- Excellent for prototyping.

#### Weaknesses

- Distribution requires Python, environment management, or a larger packaged artifact.
- Startup and runtime overhead are higher.
- Packaging across machines is less predictable than a single native binary.
- Async TUI and subprocess code still requires careful architecture.

#### Assessment

**Good for a prototype, less attractive for the intended long-term command-line tool.**

### 6.5 TypeScript and Node.js

#### Strengths

- Productive development environment.
- Good asynchronous process APIs.
- TUI frameworks such as Ink are available.

#### Weaknesses

- Requires Node.js or bundled runtime artifacts.
- Higher runtime footprint.
- A React-style TUI does not provide a decisive advantage here.

#### Assessment

**Feasible, but offers no clear advantage over Go.**

### 6.6 C#, Java, or Kotlin

These languages can implement the application reliably, but their runtime and distribution model is less attractive for a compact terminal tool. Their TUI ecosystems are also less obvious choices for this use case.

#### Assessment

**Possible, but not preferred.**

### 6.7 Decision

Git Dashboard will be implemented in **Go**.

The decision is based on:

1. the maturity of Bubble Tea and related libraries;
2. simple bounded concurrency;
3. reliable Git and lazygit process execution;
4. single-binary distribution;
5. a lower implementation cost than Rust without Python's deployment trade-offs.

---

## 7. Git Integration Strategy

### 7.1 Decision: one Git backend

Git Dashboard will use the **official Git CLI for all Git information and operations**.

It will not use `go-git` in the initial architecture.

This avoids a mixed implementation in which:

- status comes from one Git implementation;
- fetch comes from another;
- edge cases produce inconsistent results;
- behavior differs from lazygit and the user's normal Git installation.

The installed Git executable is the single source of truth.

### 7.2 Why a wrapper is not a Git reimplementation

Git Dashboard does not implement repository semantics itself. It defines a small, typed Go API that invokes documented Git commands and parses their machine-readable output.

Example interface:

```go
type GitClient interface {
    Version(ctx context.Context) (Version, error)
    Validate(ctx context.Context, path string) (Location, error)
    Inspect(ctx context.Context, location Location, options InspectOptions) (Inspection, error)
    Fetch(ctx context.Context, location Location, options FetchOptions) error
    MergeFastForwardOnly(ctx context.Context, location Location) error
}
```

The wrapper owns:

- safe argument construction;
- working-directory selection;
- the controlled child environment and process isolation (see 7.5);
- process timeouts and cancellation of complete process groups;
- stdout and stderr capture;
- parsing of documented machine-readable formats;
- conversion into domain models;
- consistent error messages.

The wrapper does not own:

- Git object traversal algorithms;
- status calculation;
- transport protocols;
- authentication;
- merging;
- rebasing;
- credential management.

### 7.3 Benefits of the official Git CLI

- Exact agreement with the user's Git installation.
- Existing Git configuration is honored.
- Existing credential helpers are honored.
- SSH agent and `~/.ssh/config` are honored.
- Worktrees, submodules, sparse checkouts, filters, and newer Git features follow official Git behavior.
- lazygit operates on the same underlying implementation.
- Network operations do not require a second authentication configuration.
- Diagnostic output is familiar to users.

### 7.4 Commands and parsing

#### Inventory validation

Inventory validation runs at startup and on a full inventory rebuild, with one Git process per candidate:

```text
git -C <path> rev-parse --path-format=absolute --git-dir --git-common-dir --show-toplevel \
    --git-path MERGE_HEAD --git-path rebase-merge --git-path rebase-apply \
    --git-path CHERRY_PICK_HEAD --git-path REVERT_HEAD --git-path BISECT_LOG \
    --git-path FETCH_HEAD
```

The command runs only for paths that contain a `.git` directory or file. A path without `.git` is classified through the file system: if it contains a `HEAD` file and `objects` and `refs` directories, it is reported as a bare repository; otherwise it is not a repository. Git itself is not invoked for bare repositories because Git refuses `-C` access to them when `safe.bareRepository = explicit` is configured.

The results are cached per entry:

- worktree root;
- Git directory;
- common Git directory, which identifies the repository store;
- marker paths for operations in progress and for `FETCH_HEAD`.

`--git-path` resolves per-worktree files correctly for linked worktrees. `rev-parse` has no NUL-delimited mode, so paths that contain a newline are rejected during validation.

#### Inspection

A refresh starts three Git processes per entry:

```text
git --no-pager -C <root> status --porcelain=v2 --branch -z --untracked-files=<normal|no>
git --no-pager -C <root> for-each-ref --format=<branch format> refs/heads
git --no-pager -C <root> config -z --get-regexp '^remote\..*\.url$'
```

The branch format is

```text
%(HEAD)%00%(refname:lstrip=2)%00%(upstream:short)%00%(upstream:track,nobracket)%00%(worktreepath)%00%(objectname)%00%(objectname:short)%00%(authorname)%00%(committerdate:unix)%00%(subject)
```

- `status` provides the branch, HEAD object ID, upstream, ahead and behind counts, and all worktree entries.
- `for-each-ref` lists every local branch, one per line, with its upstream, tracking state, the worktree that has it checked out, and its latest commit. The line marked with `*` in `%(HEAD)` supplies the latest commit of the checked-out branch. The tracking state of `for-each-ref` is untranslated plumbing output: empty, `gone`, `ahead N`, `behind N`, or `ahead N, behind M`. With `[branches].enabled = false`, the pattern is `refs/heads/<branch>`, and the call is skipped for an unborn branch or a detached HEAD.
- Only a detached HEAD, or a branch switched between `status` and `for-each-ref`, needs a fourth process for the latest commit:

  ```text
  git --no-pager -C <root> log -1 --no-color --format=%H%x00%h%x00%an%x00%ct%x00%s
  ```

  It is skipped for an unborn branch.
- `config` returns all remotes with their URLs. Exit status 1 means that no remote is configured.
- Operations in progress and the last fetch time are determined by `stat` calls on the cached marker paths, without starting additional processes. The last fetch time of an entry is the newest `FETCH_HEAD` modification time among all entries of the same repository store.

#### Status parsing rules

- `# branch.oid (initial)` means an unborn branch.
- `# branch.head (detached)` means a detached HEAD.
- A missing `# branch.upstream` line means no upstream.
- A `# branch.upstream` line without a `# branch.ab` line means that the upstream is gone.
- Unknown header lines (`# ...`) are ignored, as required by Git's porcelain v2 documentation.
- Entry types `1`, `2`, `u`, `?`, and `!` are parsed. An unknown entry type is a compatibility error: the worktree state of that repository becomes unknown and is never reported as clean.

Counting rules:

| Count | Rule |
|---|---|
| staged | entries of type `1` or `2` whose index status `X` is not `.` |
| modified | worktree status `Y` is `M` or `T` |
| deleted | worktree status `Y` is `D`; staged deletions count as staged |
| renamed | entries of type `2`, counted in addition to staged |
| conflicts | entries of type `u` |
| untracked | entries of type `?` |

#### General rules

- Never parse localized, human-oriented output when a machine-readable format exists.
- Use NUL-delimited output where available.
- Set format strings explicitly.
- Do not depend on column spacing.
- Do not parse colors.
- Disable pagers.
- Do not parse Git's error messages. Stderr is displayed verbatim.
- Keep parsers covered by fixture and integration tests.

### 7.5 Environment and process isolation

Every Git process that runs while the TUI owns the terminal is started as follows:

- `GIT_TERMINAL_PROMPT=0`, so Git does not prompt for credentials.
- `GIT_OPTIONAL_LOCKS=0` for read-only commands, so that `git status` neither takes `index.lock` nor rewrites the index while lazygit, an editor, or a shell works in the same repository.
- `--no-pager` and `GIT_PAGER=cat`.
- `GIT_EDITOR=true`, so that no command can open an editor.
- Standard input is `/dev/null`; standard output and standard error are captured.
- The process starts in a new session (`setsid`) without a controlling terminal. ssh and credential helpers therefore cannot open `/dev/tty`. A prompt fails instead of writing into the TUI. A running ssh-agent, credential helpers, and configured `SSH_ASKPASS` or `GIT_ASKPASS` programs keep working.
- The new session also makes the process the leader of its own process group. Timeouts and cancellation terminate the whole group, including ssh and remote helpers: first with `SIGTERM`, so that Git removes its lock files (for example `index.lock` during a fast-forward), and with `SIGKILL` if the group has not exited after a grace period of 3 seconds.
- After Git exits, Git Dashboard waits at most 2 seconds for its output pipes. A grandchild that keeps them open, such as an ssh `ControlPersist` master, therefore cannot turn a successful command into a timeout.
- All text from external sources, such as commit subjects, author names, remote URLs, Git error output, and directory names, is displayed with control characters replaced, so it cannot inject terminal escape sequences.
- `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`, `GIT_COMMON_DIR`, `GIT_OBJECT_DIRECTORY`, and `GIT_NAMESPACE` are removed from the child environment. If inherited, they would override `-C` and redirect every command to the same repository. The same applies to lazygit and launchers.
- The locale and all other environment variables are inherited unchanged.

Git Dashboard does not globally override authentication or repository configuration.

Interactive Git commands, such as retrying a failed fetch in the terminal, are started through the terminal handover described in chapter 8. They are attached to the terminal and do not suppress prompts.

At startup, `git version` is executed once. If the version is older than 2.34, Git Dashboard exits with a clear error message.

### 7.6 Repository validation

Discovery locates candidates through `.git` directories or files. Every candidate is validated with the inventory command from 7.4. This correctly handles linked worktrees, where `.git` is a file rather than a directory.

Validation outcomes:

| Outcome | Explicit entry | Discovered candidate |
|---|---|---|
| valid worktree root | available | included |
| path below the worktree root | `config error` naming the actual root | not applicable, because discovery stops at the root |
| bare repository (detected through the file system) | `unsupported: bare repository` | never a candidate, because it has no `.git` entry |
| not a repository | `not a repository` | ignored |
| path does not exist | `missing` | not applicable |
| validation fails and the directory belongs to another user | `untrusted owner`, with a hint to Git's `safe.directory` | listed with `untrusted owner` |

Git Dashboard never modifies `safe.directory` or any other Git configuration.

---

## 8. lazygit Handover

### 8.1 User experience

The transition should feel like moving from the collection view into the selected repository:

1. The user selects a repository.
2. The user presses `Enter`.
3. Git Dashboard pauses and releases the terminal.
4. lazygit starts for the selected local path.
5. The user works normally in lazygit.
6. The user exits lazygit.
7. Git Dashboard restores its terminal mode.
8. The selected repository is reinspected.

### 8.2 Process model

lazygit runs as a synchronous child process with:

- the selected repository as its working directory;
- the current terminal's standard input;
- the current terminal's standard output;
- the current terminal's standard error;
- the user's existing environment, without the repository-redirecting `GIT_*` variables listed in 7.5.

Conceptually:

```go
cmd := exec.Command(lazygitExecutable, lazygitArgs...)
cmd.Dir = worktreeRoot
cmd.Env = sanitizedEnvironment()
cmd.Stdin = os.Stdin
cmd.Stdout = os.Stdout
cmd.Stderr = os.Stderr
```

Using the child process working directory is preferred over depending on a lazygit-specific path flag. It uses a universal process mechanism and lets lazygit discover the repository as it normally would.

lazygit is deliberately not bound to a cancellable context. Cancelling dashboard work must never terminate an interactive session.

The implementation uses Bubble Tea's child-process execution mechanism (`tea.ExecProcess`). Git Dashboard does not emit its own sequence of terminal escape codes.

The same handover is used for every interactive child process: lazygit, retrying a fetch in the terminal, and configured launchers.

### 8.3 Terminal ownership

Before lazygit starts:

- stop new rendering;
- suspend Git Dashboard keyboard handling;
- leave raw mode through the TUI framework;
- leave the alternate screen through the TUI framework;
- restore cursor state through the TUI framework.

While lazygit runs:

- lazygit exclusively owns terminal input and output;
- Git Dashboard does not render;
- no new job starts for the repository store that is open in lazygit; queued jobs for that store wait until lazygit exits.

#### Background jobs during the handover

- Batch jobs that are already running continue. They cannot access the terminal (see 7.5).
- Job workers never block on delivery to the user interface. They report to the scheduler, which stores job state and buffers events. A single forwarder delivers the events to Bubble Tea after the handover. This matters because the Bubble Tea event loop does not process messages while a child process runs.
- Opening a repository store that has an active mutating job (fetch or update) is refused with a message such as `fetch in progress for work/api`. The user can wait or cancel the job.

After lazygit exits:

- restore the TUI terminal mode;
- obtain the current terminal size again;
- reinspect the repository regardless of lazygit's exit status;
- retain the active filter and selection if possible;
- display a concise error if the process failed.

### 8.4 Why lazygit is a separate process

Embedding lazygit would couple Git Dashboard to lazygit's internal packages and UI lifecycle. Those packages are not the product boundary lazygit promises to external consumers.

A process handover provides:

- stable operating-system boundaries;
- independent upgrades;
- no fork maintenance;
- the user's normal lazygit configuration;
- the user's normal lazygit version and plugins;
- clear terminal ownership.

### 8.5 Failure cases

#### lazygit is unavailable

The executable is resolved with `exec.LookPath` at startup. If it is missing, `Enter` does not attempt a handover. Git Dashboard reports:

- the executable it attempted to start;
- that it was not found or was not executable;
- where the command can be configured.

The dashboard remains usable.

#### lazygit exits unsuccessfully

- Show the exit status and concise error.
- Reinspect the repository anyway because changes may have occurred before failure.

#### Repository is moved or removed

- Mark the entry unavailable.
- Reconcile it during the next full inventory refresh.

#### Terminal restoration fails

- Return an explicit fatal error rather than continuing with a corrupted UI.
- Ensure the process exit path attempts framework-supported cleanup.

### 8.6 lazygit configuration

```toml
[lazygit]
command = "lazygit"
args = []
enabled = true
```

- `command` is either an executable name that is looked up in `PATH` or an absolute path.
- `args` is an optional argument array. No shell is involved, and no placeholders are expanded.
- At startup, the command must resolve to a regular executable file. Otherwise, lazygit is reported as unavailable and the dashboard remains usable.

---

## 9. Target Architecture

### 9.1 Components

#### Configuration

- Load and validate TOML.
- Resolve configuration paths.
- Provide defaults.
- Represent explicit repositories, discovery roots, families, exclusions, concurrency, and launchers.

#### Inventory

- Combine explicit and discovered repositories.
- Canonicalize paths.
- Deduplicate entries.
- Apply include, exclude, and override precedence.
- Report unavailable configured entries.

#### Git client

- Invoke the official Git executable.
- Parse machine-readable output.
- Return domain-level repository state.
- Run fetch and fast-forward-only merge.
- Preserve stdout, stderr, and exit status in errors.

#### Job scheduler

- Limit concurrent inspection and batch jobs.
- Support cancellation through `context.Context`.
- Apply per-job timeouts.
- Serialize mutating operations per repository store (common Git directory).
- Deduplicate fetch jobs that target the same repository store.
- Block jobs for a repository store while an interactive handover is open for it.
- Store job state and buffer progress events; workers never block on the user interface.

#### Terminal launcher

- Check executable availability at startup.
- Coordinate the terminal handover.
- Run lazygit, interactive fetch retries, and configured launchers as foreground child processes.
- Return the process outcome.

#### TUI

- Manage views and interaction.
- Consume domain models and application services.
- Never construct Git commands directly.

### 9.2 Dependency direction

```text
cmd/git-dashboard
        |
        v
application services
        |
        +--> domain
        +--> inventory ports
        +--> git client port
        +--> scheduler port
        +--> launcher port
                   ^
                   |
infrastructure adapters
        +--> filesystem inventory
        +--> Git CLI client
        +--> terminal handover launcher

tui --> application services + domain
```

The domain does not depend on Bubble Tea, process execution, or Git output formats.

---

## 10. Project Structure

```text
git-dashboard/
├── cmd/
│   └── git-dashboard/
│       └── main.go              # flags, configuration lookup, version check, --check
├── internal/
│   ├── app/                     # application services
│   │   ├── application.go
│   │   ├── refresh.go           # inspection jobs, store-wide fetch times
│   │   ├── fetch.go             # one fetch job per repository store
│   │   ├── update.go            # eligibility, preview, recheck, fast-forward jobs
│   │   └── view.go              # filters and sorting
│   ├── config/
│   │   ├── config.go
│   │   ├── defaults.go
│   │   ├── loader.go            # lookup, strict decoding, path expansion
│   │   └── validation.go
│   ├── domain/
│   │   ├── repository.go
│   │   └── operation.go
│   ├── inventory/
│   │   ├── inventory.go         # precedence rules and validation
│   │   ├── discovery.go
│   │   ├── patterns.go
│   │   └── identity.go
│   ├── git/
│   │   ├── client.go
│   │   ├── command.go           # isolated runner, process-group termination
│   │   ├── environment.go
│   │   ├── process_unix.go
│   │   ├── version.go
│   │   ├── validate.go
│   │   ├── inspect.go
│   │   ├── status_parser.go
│   │   ├── markers.go
│   │   ├── remotes.go
│   │   ├── fetch.go
│   │   └── update.go
│   ├── jobs/
│   │   ├── scheduler.go
│   │   ├── event.go             # unbounded event queue
│   │   └── store_lock.go
│   ├── launcher/
│   │   ├── launcher.go
│   │   ├── lazygit.go
│   │   ├── custom.go
│   │   └── browser.go
│   ├── tui/
│   │   ├── model.go
│   │   ├── update.go
│   │   ├── view.go              # panels, header, footer, and dialogs
│   │   ├── status.go
│   │   ├── layout.go            # bordered panels, cells, overlays, progress bars
│   │   ├── theme.go             # styles derived from [ui.theme]
│   │   ├── icons.go             # unicode, nerd, and ascii symbol sets
│   │   ├── keymap.go
│   │   └── messages.go
│   └── testutil/                # real Git repositories for tests
├── testdata/
│   └── git-output/
├── examples/
│   └── config.toml
├── docs/
│   ├── design.md
│   ├── configuration.md
│   └── keybindings.md
├── .github/workflows/ci.yml
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

The interactive fetch command is provided by the Git client (`InteractiveFetchCommand`); the views share one file because they are small. A license has not been chosen yet.

---

## 11. Domain Model

```go
type Repository struct {
    ID             RepositoryID // canonical worktree root
    StoreID        StoreID      // canonical common Git directory
    Name           string
    Path           string
    Family         FamilyID
    Source         InventorySource
    ExpectedRemote *ExpectedRemote
    Untracked      UntrackedMode
    Availability   Availability
    EntryError     error // configuration or validation problem of this entry

    Inspection     InspectionInfo
    Head           Known[HeadState]
    Operation      Known[OperationInProgress]
    Worktree       Known[WorktreeState]
    Upstream       Known[UpstreamState]
    Remotes        Known[[]Remote]
    PrimaryRemote  *Remote
    LastCommit     Known[*CommitSummary] // nil for an unborn branch
    LastFetch      Known[*time.Time]     // nil if never fetched
    BranchPolicy   BranchPolicy
    Branches       Known[[]BranchState] // other local branches that need attention
}
```

Selected supporting models:

```go
// Known distinguishes "not determined" from a valid zero value.
type Known[T any] struct {
    Value T
    Valid bool
}

type InspectionInfo struct {
    Generation uint64
    FinishedAt time.Time
    Err        error
}

type InventorySource string

const (
    InventorySourceExplicit   InventorySource = "explicit"
    InventorySourceDiscovered InventorySource = "discovered"
)

type Availability int

const (
    Available Availability = iota
    Missing
    NotARepository
    NotWorktreeRoot // explicit path points below the worktree root
    BareRepository
    UntrustedOwner
)

type HeadState struct {
    Branch   string
    CommitID string
    Detached bool
    Unborn   bool
}

type OperationInProgress int

const (
    NoOperation OperationInProgress = iota
    Merging
    Rebasing
    CherryPicking
    Reverting
    Bisecting
)

type WorktreeState struct {
    Staged           int
    Modified         int
    Deleted          int
    Renamed          int
    Conflicts        int
    Untracked        int
    UntrackedChecked bool
}

func (w WorktreeState) Clean() bool // derived; untracked entries count as dirty

type UpstreamState struct {
    Configured bool
    Ref        string
    Gone       bool
    Ahead      int
    Behind     int
}

func (u UpstreamState) Diverged() bool { return u.Ahead > 0 && u.Behind > 0 }

type BranchState struct {
    Name       string
    Upstream   string // empty if none
    Gone       bool
    Ahead      int
    Behind     int
    CommitTime time.Time
    Current    bool   // checked out in this worktree
    Worktree   string // other worktree that has the branch checked out
    Pinned     bool
    Missing    bool   // configured pinned branch that does not exist
}

type BranchPolicy struct {
    Enabled       bool
    Pinned        []string
    ReportMissing bool // only for per-repository pinned lists
    Ignore        []string
    MaxAge        time.Duration
}

// Select returns the branches other than HEAD that need attention.
func (p BranchPolicy) Select(all []BranchState, now time.Time) []BranchState

type OperationResult struct {
    RepositoryID RepositoryID
    StoreID      StoreID
    Operation    OperationType
    Status       OperationStatus
    StartedAt    time.Time
    FinishedAt   time.Time
    ExitCode     *int
    Stderr       string // bounded
    Error        error
}
```

Rules:

- Unknown state is distinct from a successful empty value. A failed inspection sets `Valid = false`; it never produces `Behind = 0` or a clean worktree.
- Derived values such as `Clean` and `Diverged` are computed, not stored, so they cannot contradict their inputs.
- `InspectionInfo.Generation` ensures that results of an older refresh never overwrite newer state.

---

## 12. Configuration Format

### 12.1 TOML versus YAML

#### Decision: TOML

TOML is better suited to Git Dashboard because this is a human-maintained application configuration rather than a general data-serialization format.

Advantages of TOML:

- clear distinction between strings, numbers, booleans, arrays, and tables;
- fewer surprising implicit conversions than YAML;
- paths, URLs, branch names, and timestamps are easier to read safely;
- repeated tables map naturally to repositories and discovery roots;
- comments are supported;
- whitespace has no structural meaning;
- configuration remains approachable without becoming overly terse.

YAML would be reasonable if the configuration required deeply nested structures, anchors, or integration with an existing YAML-heavy ecosystem. Git Dashboard does not currently need those features.

### 12.2 Location

The configuration file is resolved in this order:

1. the `--config <path>` command-line option;
2. the `GIT_DASHBOARD_CONFIG` environment variable;
3. `$XDG_CONFIG_HOME/git-dashboard/config.toml`, falling back to `~/.config/git-dashboard/config.toml`.

The same lookup is used on Linux and macOS. If no file exists, Git Dashboard starts with an empty inventory and shows the expected path together with a pointer to the example configuration.

### 12.3 Path expansion

All path values (`path`, `root`, `[git].executable`, `[lazygit].command`) support:

- `~` and `~/` at the beginning of the path;
- environment variables in the form `${NAME}`; an undefined variable is a configuration error.

Other shell syntax is not interpreted. Relative paths are resolved against the directory of the configuration file.

### 12.4 Example configuration

```toml
version = 1

[[repositories]]
path = "~/devrepos/dotfiles"
family = "Personal"

[[repositories]]
path = "~/devrepos/company/api"
family = "Work"
remote = "git@github.com:example/api.git"
remote_name = "origin"

[[repositories]]
path = "~/devrepos/experiments/parser"
enabled = false

[[repositories]]
path = "~/devrepos/company/monorepo"
family = "Work"
untracked = "no"
pinned_branches = ["main", "develop"]

[[discovery]]
root = "~/devrepos/company"
max_depth = 2
family = "Work"
include = ["**"]
exclude = [
  "archive/**",
  "**/node_modules/**",
  "**/vendor/**"
]
follow_symlinks = false

[[discovery]]
root = "~/devrepos/experiments"
max_depth = 3
family = "Experiments"
exclude = ["discarded/**"]
follow_symlinks = false

[git]
executable = "git"
inspection_parallelism = 8
inspection_timeout = "10s"
fetch_parallelism = 4
fetch_timeout = "120s"
fetch_prune = true
untracked = "normal"

[branches]
enabled = true
pinned = ["main", "master"]
ignore = ["archive/**", "dependabot/**"]
max_age = "30d"

[lazygit]
command = "lazygit"
args = []
enabled = true

[[launchers]]
name = "Editor"
command = ["nvim", "."]
```

### 12.5 Configuration rules

- Explicit repository paths may be absolute or relative to the configuration file.
- Discovery roots may be absolute or relative to the configuration file.
- Include and exclude patterns are evaluated relative to their discovery root (see 3.4).
- Explicit repository configuration overrides discovery settings (see 3.5).
- Repositories without a family belong to the built-in family `Ungrouped`.
- `untracked` accepts `normal` or `no`. The value in `[git]` is the default; a repository entry may override it. `no` passes `--untracked-files=no` to `git status` for very large repositories.
- Durations use Go duration syntax, for example `"10s"` or `"2m"`.
- `[branches].max_age` additionally accepts a number of days such as `"30d"`, and `"0"` to disable the age filter. A repository's `pinned_branches` replaces `[branches].pinned` (see 5.2, "Local branches other than HEAD").
- Unknown fields are rejected with a useful error during the pre-1.0 phase to catch spelling mistakes.
- Syntax and type errors in the file prevent startup. Problems with individual entries, such as missing paths, appear as entry states and do not prevent startup.
- Missing explicit repositories remain visible as unavailable entries.
- Missing discovery roots produce warnings and no discovered entries.
- The target set of a batch action is the set of marked repositories if any are marked; otherwise it is the set of currently visible rows (see 13.2).

---

## 13. TUI Concept

### 13.1 Main view

The layout follows lazygit's panel style: every panel has a rounded border with its title embedded in the top border and secondary information in the bottom border. The focused panel uses the active border color. Terminals with at least 130 columns show the repository table and the detail panel side by side; narrower terminals stack them.

```text
 Git Dashboard   1 dirty 1  2 behind 2  3 ahead 1  4 diverged 1  5 errors 0  6 branches 0
╭─ Repositories ─────────────────────────────────────────────────────────────╮╭─ api ────────────────────────────────────────────╮
│   Repository   Family Branch     Status           Upstream         Commit  ││ ~/devrepos/company/api                           │
│▌  api        ↓ Work   main       ✓ clean  ≠remote ↓3               2h      ││ family    Work                                   │
│   billing    ✘ Work   main       ✓ clean          ↑1 ↓1 diverged   3h      ││                                                  │
│ ◆ frontend   ● Work   develop    ✚1  ●2  ?3       ✓                3h      ││ BRANCH                                           │
│   tools      ✗ Work   –          ✗ missing        –                        ││ main → origin/main  ↓3                           │
│                                                                            ││                                                  │
│                                                                            ││ WORKTREE                                         │
│                                                                            ││ ✓ clean                                          │
│                                                                            ││                                                  │
│                                                                            ││ LAST COMMIT                                      │
│                                                                            ││ a3f91c2  Fix request validation                  │
│                                                                            ││ Test · 2 h ago                                   │
│                                                                            ││ fetched 10 min ago                               │
│                                                                            ││                                                  │
│                                                                            ││ REMOTES                                          │
│                                                                            ││ origin  git@github.com:ex/api.git                │
│                                                                            ││ ≠ expected origin: git@github.com:company/api.git│
│                                                                            ││                                                  │
╰─────────────────────────────────────── 1 marked · A: clear marks │ 1 of 4 ─╯╰─────────────────────────────────────── explicit ─╯
 Enter lazygit │ f fetch marked │ u update marked │ A clear marks │ / filter │ ? all keys
```

- The header shows counters for the current family. The digits `1`–`6` apply the corresponding state filter; pressing the digit again clears it.
- Other local branches that need attention appear as a count such as `⎇2` after the upstream state, separated by `│`; the detail panel lists them (see 5.2).
- A one-character health column after the name shows the most important state of the row: errors and unavailable entries, conflicts, operations in progress, or divergence first, then a dirty worktree, behind, ahead, and other attention (`!`, such as a remote mismatch or other branches). Only healthy repositories show `✓`; their names are not emphasized, so rows that need attention stand out.
- The `Commit` column shows the age of the latest commit.
- In the tree, repositories are connected to their family row by guide lines (`├─`, `└─`). Each family row, and the detail panel of a family row, summarizes the shown repositories by their health level, such as `✗1 error  ●2 dirty`, or shows `✓ all healthy`; every repository counts once, at its most important level.
- The right side of the header shows a spinner while the inventory loads or repositories are inspected, and a progress bar while a batch runs. Rows of repositories with a running job show a spinner in the upstream column.
- The footer shows only the most important key hints for the current mode and context, `?` lists all keys. With marks, fetch and update name the marked repositories and `A` clears the marks; on a family row `Enter` expands or collapses; an active text filter adds `Esc`. The operations view shows cancel while the batch runs, retry when it has failed rows, continue for a finished update, and the terminal retry for a selected failed fetch. Messages appear at the right end of the footer and disappear after 4 seconds (errors after 10 seconds); details remain available through `e` and `b`.
- Dialogs (help, errors, sorting, operations, confirmations, update preview, launchers) are centered boxes over a dimmed copy of the main view.
- The operations view shows a progress bar with percentage and counts, and groups the repositories by status: running, failed, cancelled, skipped, queued, and completed. `j`/`k` follow this order; the selection stays on its repository when the repository moves to another group.
- Missing and otherwise unavailable entries keep their normal sort position and show their state in the status column.
- A remote mismatch is shown as a short `≠remote` marker in the status column; the detail panel shows the configured and the actual URL.
- Linked worktrees of the same store are listed in the detail panel.
- The detail panel groups its content into titled sections: branch, other branches, worktree (nonzero counts only), last commit and fetch time, remotes, linked worktrees, and errors last. Labels and remote names are aligned. If the panel is too small, blank lines are dropped first, then the section titles.
- Column widths follow the content of all visible rows, within per-column limits; space left over stays empty at the end of a row. If space is short, branch, name, and family shrink before status and upstream.
- Colors and the symbol set are configurable in `[ui]` (see `docs/configuration.md`).

### 13.2 Key bindings

| Key | Action | Phase |
|---|---|---|
| `j` / `↓` | next row | 1 |
| `k` / `↑` | previous row | 1 |
| `g` / `G` | first / last row | 1 |
| `PgUp` / `PgDn` | page up / down | 1 |
| `/` | text filter | 1 |
| `1` … `5` | quick filter: dirty, behind, ahead, diverged, errors | 1 |
| `6` | quick filter: other branches that need attention | after 5 |
| `Esc` | clear the text filter or close the current overlay | 1 |
| `r` | refresh visible repository state | 1 |
| `R` | reload configuration, rebuild inventory, and refresh all | 1 |
| `s` | choose sorting | 1 |
| `e` | show error details | 1 |
| `?` | help | 1 |
| `q` | quit | 1 |
| `Enter` | open selected repository in lazygit | 2 |
| `Tab` | switch family | 3 |
| `Space` | toggle mark | 3 |
| `a` | mark all visible repositories | 3 |
| `A` | clear all marks | 3 |
| `f` | fetch marked repositories, or the repository under the cursor if none are marked | 3 |
| `F` | fetch visible repositories | 3 |
| `c` | cancel the running batch (operations view) | 3 |
| `x` | retry failed operations (operations view) | 3 |
| `b` | reopen the operations view | 3 |
| `t` | retry the selected failed fetch in the terminal (operations or error view) | 3 |
| `u` | open safe-update preview for marked or visible repositories | 4 |
| `Enter` | open the selected row in lazygit (operations view) | 4 |
| `u` | continue a finished update batch with a new preview (operations view) | 4 |
| `o` | open launcher menu | 5 |

Input modes prevent conflicts between text entry and global shortcuts.

The text filter consists of space-separated tokens that must all match. Plain tokens match name or path case-insensitively. State tokens filter by repository state: `is:dirty`, `is:clean`, `is:behind`, `is:ahead`, `is:diverged`, `is:noupstream`, `is:error`, `is:unavailable`, `is:mismatch`, `is:marked`, `is:branches`, and `is:unpublished`.

Marks persist when filters change. A batch action on marked repositories always includes all marked repositories; the confirmation view states how many of them are hidden by the current filter.

### 13.3 Status representation

Color is never the only information carrier. Every state is expressed by a symbol with a count or by text, and the help view contains a legend. Colors are applied per cell rather than per row, so problems stand out while clean repositories stay unobtrusive. Status symbols are separated by two spaces. Colors follow a small hierarchy: neutral gray for clean states and secondary text, the accent color for the cursor, keys, branch names, and running operations, yellow for local changes and stale or unexpected state, red for conflicts, divergence, and failures, and a separate success color for finished operations, so that success is not confused with commits ahead.

| Unicode | Meaning | Default color |
|---|---|---|
| `✓ clean` | clean worktree | gray |
| `✚n` | staged entries | green |
| `●n` | modified entries | yellow |
| `−n` | deleted entries | yellow |
| `?n` | untracked entries | gray |
| `✘n` | conflicting entries | red |
| `merging`, `rebasing`, `cherry-picking`, `reverting`, `bisecting` | operation in progress | red |
| `↑n` / `↓n` | ahead of / behind the upstream | green / magenta |
| `↑n ↓m diverged` | diverged | red |
| `✓` | up to date | gray |
| `∅ no upstream`, `∅ upstream gone`, `⌀ detached`, `no commits` | upstream and HEAD states | gray / yellow |
| `✗ missing`, `✗ not a repository`, `✗ config error`, `✗ unsupported: bare repository`, `✗ untrusted owner` | unavailable entries | red |
| `✗ fetch failed` | last fetch failed | red |
| `⎇n` | other local branches need attention | yellow if any holds unpublished work, otherwise magenta |
| `≠remote` | remote mismatch | yellow |
| `!` | health column: other attention, such as a remote mismatch or other branches | yellow, or magenta if no branch holds unpublished work |
| `◆` | marked | cyan |
| `▌` | cursor | cyan (`selected_accent`); the selected row uses a subtle background on which names and secondary text take the selected foreground, while semantic colors are kept |

Three symbol sets are available through `[ui].icons`: `unicode` (default), `nerd` for Nerd Font icons, and `ascii` for limited terminals. The `NO_COLOR` environment variable disables all colors.

---

## 14. Concurrency and Consistency

### Inspection

- Inspect repositories concurrently, bounded by `git.inspection_parallelism`.
- Apply `git.inspection_timeout` per inspection.
- Stream results incrementally to the TUI.
- Associate results with a refresh generation so stale results cannot overwrite newer state.
- Read-only commands run with `GIT_OPTIONAL_LOCKS=0` and therefore do not conflict with other Git processes.

### Operations

- Locks are keyed by repository store (common Git directory), not by worktree path.
- Allow at most one mutating operation per repository store.
- A fetch that targets several worktrees of the same store runs once.
- Fetches for different stores may run concurrently, bounded by `git.fetch_parallelism`.
- An open interactive handover (lazygit, interactive fetch, launcher) locks its store against new dashboard operations.
- Cancelling a batch prevents new jobs and terminates running commands, including their complete process groups.
- Workers report to the scheduler and never block on the user interface (see 8.3).

### Refresh behavior

- After a Git operation, reinspect all entries of the affected repository stores.
- After an interactive handover, always reinspect all entries of the opened store.
- A full inventory rebuild is available separately because it may add or remove discovered repositories.

---

## 15. Error Handling

### Principles

- Capture errors per repository.
- One failed repository must not make the dashboard unusable.
- Do not replace errors with plausible successful values.
- Retain bounded stderr output for failed child processes.
- Do not duplicate credentials or sensitive environment values in logs.
- Include operation, repository, and root cause in errors.

Examples:

```text
work/api: fetch failed: git exited with status 128: <stderr shown verbatim>
personal/dotfiles: inspection failed: unable to resolve HEAD
work/frontend: update skipped: worktree contains local changes
/path/to/devrepos/tools: configured repository is missing
```

### Logging

- Keep default logging quiet.
- Offer optional debug logging to a configured file.
- Do not write uncontrolled output while the TUI is active.
- Log command arguments only when they contain no sensitive values.
- Redact user information in URLs (`https://user:token@host/...`) before displaying or logging them.

---

## 16. Security

- Never interpolate repository paths into a shell command.
- Start processes with an executable and argument list.
- Treat repository paths and remote URLs as data.
- Resolve configured executables at startup; they must be regular executable files.
- Remove repository-redirecting `GIT_*` variables from child environments (see 7.5).
- Background Git processes cannot access the terminal and cannot prompt for credentials (see 7.5).
- Recheck all `Update eligible` preconditions immediately before each merge.
- Use `merge --ff-only --no-autostash` so that a configured auto-stash cannot change the worktree.
- Never perform automatic stash, merge, rebase, reset, or force-push.
- Require a confirmation view for batch operations that alter local branches.
- Show the exact repository count before `Fetch visible`.
- Do not silently modify a remote when an expected URL differs.
- Never modify `safe.directory` or any other Git configuration.
- Do not automatically clone a missing repository in the MVP.

---

## 17. Test Strategy

### 17.1 Unit tests

- Configuration parsing and validation.
- Inventory precedence.
- Include and exclude patterns.
- Canonical-path deduplication.
- Family assignment.
- Filters and sorting.
- Status parser fixtures, including unknown header lines and unknown entry types.
- Counting rules for staged, modified, deleted, renamed, conflicting, and untracked entries.
- Path expansion of `~` and `${NAME}`.
- Update eligibility rules.
- Fetch deduplication per repository store.
- Environment sanitization of child processes.
- Scheduler concurrency limits.
- TUI update logic.
- Git command construction without a shell.

### 17.2 Integration tests

Create temporary real Git repositories and verify:

- clean and dirty worktrees;
- staged, unstaged, deleted, and untracked files;
- merge conflicts;
- merge, rebase, cherry-pick, revert, and bisect in progress;
- normal branch, detached HEAD, and unborn branch;
- missing upstream and upstream gone;
- ahead, behind, and diverged states;
- multiple remotes and no remote;
- linked worktrees, including one fetch per store and the per-worktree `FETCH_HEAD`;
- bare repositories, explicit paths below a worktree root, invalid and missing repositories;
- explicit and discovered inventory merging, including the precedence table;
- repositories at different directory depths;
- pruning of excluded directories;
- symbolic-link loops with `follow_symlinks = true`;
- case-insensitive deduplication on macOS;
- inspection does not create `index.lock` (`GIT_OPTIONAL_LOCKS=0`);
- a fetch whose `GIT_SSH_COMMAND` tries to read from `/dev/tty` fails promptly instead of hanging;
- timeout and cancellation terminate the complete process group;
- fetch success and failure;
- `merge --ff-only` accepts a fast-forward;
- `merge --ff-only` rejects divergence;
- an inherited `GIT_DIR` does not redirect inspection.

Integration tests run in CI against Git 2.34 and the latest Git release.

Expected state should be compared with official Git behavior because the official Git executable is the application's backend.

### 17.3 lazygit handover tests

First test process integration with a controlled helper executable instead of lazygit. The helper:

- reports its working directory;
- verifies terminal input and output attachment;
- returns configurable exit codes;
- optionally changes a test repository.

The helper is also used to verify that job events produced while the child process runs are delivered after the handover and that workers never block.

Then perform a manual terminal test with real lazygit:

- alternate screen is handled correctly;
- keyboard input works;
- terminal resize works;
- returning redraws Git Dashboard;
- repository state is refreshed;
- errors remain visible.

### 17.4 Performance test

A benchmark creates 200 generated repositories of moderate size on the local file system and verifies that inspection of all of them completes within 2 seconds and stays within the process budget from 5.4.

### 17.5 Supported platforms

Supported:

- Linux
- macOS

Not supported:

- Windows

The code should avoid gratuitous platform coupling, but no Windows-specific compatibility work or release validation is planned.

---

## 18. Delivery Plan

### Phase 0: Technical spike

Goal: validate the critical assumptions with minimal code.

- Create a Bubble Tea skeleton.
- Inspect one test repository through the Git CLI wrapper with the three inspection commands.
- Start lazygit or a helper process through `tea.ExecProcess`.
- Restore the TUI after the child process exits.
- Run `git fetch` in a new session without a controlling terminal, with stdout and stderr captured.
- Produce job events while a child process owns the terminal.

Acceptance criteria:

- The terminal remains usable after handover.
- Repository state can be read before and after the child process.
- No custom low-level terminal protocol is required.
- Structured Git output can be parsed without localized text.
- A fetch that needs a prompt fails without touching the terminal.
- Job events produced during the handover are delivered afterward, and no worker blocks.

### Phase 1: Read-only MVP

- TOML configuration, configuration lookup, and path expansion.
- Git version check.
- Explicit repository inventory.
- Bounded discovery with inclusion and exclusion rules.
- Repository identity, worktree stores, and validation states.
- Domain model and Git client with process isolation.
- Concurrent inspection.
- Main table and detail pane.
- Filters, sorting, and refresh.
- Error and missing-repository states.

Acceptance criteria:

- A mixed directory tree produces the intended repository inventory.
- Repositories at different depths are supported.
- Excluded repositories do not appear, and excluded directories are not traversed.
- Branch, remote, latest commit, operation-in-progress, and worktree state match Git in the defined test cases.
- 200 repositories are inspected within 2 seconds.

### Phase 2: lazygit integration

- Launcher interface and terminal handover.
- Executable availability check.
- Targeted refresh after return.
- Exit-status reporting.
- Store lock while lazygit is open.

Acceptance criteria:

- lazygit opens in the selected repository.
- Git Dashboard is fully usable after lazygit exits.
- Changes made through lazygit appear after returning.

### Phase 3: Families and batch fetch

- Family configuration.
- Multi-selection.
- Bounded job scheduler with store locks, deduplication, and timeouts.
- Fetch marked, fetch cursor row, and fetch visible.
- Progress view, cancellation, and retry.
- Interactive fetch retry in the terminal.
- Refresh of upstream state.

Acceptance criteria:

- A failure does not stop unrelated fetch jobs.
- Maximum parallelism is respected.
- Every result remains inspectable.
- Only visible or marked repositories are targeted.
- A repository store is fetched at most once per batch.
- A fetch that needs authentication can be completed through the interactive retry.

### Phase 4: Safe update

- Eligibility checks.
- Preview with skip reasons and last fetch age.
- `merge --ff-only --no-autostash @{upstream}`.
- Recheck before each merge.
- Integration tests for fast-forward and divergence.

Acceptance criteria:

- Only safe candidates are changed.
- Dirty, diverged, detached, unborn, upstream-less, and in-progress repositories are skipped.
- Git Dashboard never creates a merge commit, a rebase, or a stash.

### Phase 5: Additional launchers

- Launcher configuration and menu.
- `{path}` placeholder handling.
- `Open remote in browser`.

Acceptance criteria:

- Launchers start in the worktree root without involving a shell.
- The repository is reinspected after a launcher exits.

### Phase 6: Distribution and stabilization

- Linux and macOS builds.
- Installation documentation.
- Example configuration.
- Performance test with many repositories.
- Release artifacts and checksums.

---

## 19. Decisions and Open Questions

### Confirmed decisions

- Product name: Git Dashboard.
- Implementation language: Go.
- TUI: Bubble Tea and appropriate Charm libraries.
- Git backend: official Git CLI only, version 2.34 or newer.
- Git access: typed internal wrapper using machine-readable output.
- Background Git processes: non-interactive, without a controlling terminal, own process group, `GIT_OPTIONAL_LOCKS=0` for reads.
- Detailed repository work: lazygit as a synchronous child process.
- Repository inventory: explicit local paths plus optional bounded discovery.
- Inventory precedence: explicit entries always win; `enabled = false` suppresses a repository.
- Families: assigned explicitly per repository or discovery entry; otherwise `Ungrouped`. Never derived from directory names.
- Nested repositories: only through explicit entries; discovery never descends into a repository.
- Explicit paths must be worktree roots; a subdirectory is reported as a configuration error.
- Linked worktrees: separate rows, grouped into a repository store for locks and fetches.
- Bare repositories: not supported as entries.
- Remote URLs: optional assertions, not primary identity. A mismatch appears as a marker in the table and in detail in the detail pane.
- Missing explicit repositories keep their normal sort position.
- Existing local checkout required for the MVP.
- Configuration format: TOML, located through `--config`, `GIT_DASHBOARD_CONFIG`, or the XDG configuration directory.
- Batch targets: marked repositories if any are marked; otherwise visible repositories (`F`) or the cursor row (`f`).
- Last fetch time: modification time of `FETCH_HEAD`.
- Local branches other than HEAD: all local branches are evaluated, but only those that need attention, are recent enough, and are not ignored are reported; pinned branches bypass these filters. See 5.2, "Local branches other than HEAD".
- Safe update: `merge --ff-only --no-autostash @{upstream}` after explicit eligibility checks; no network access.
- Performance target: 200 repositories inspected within 2 seconds.
- Supported platforms: Linux and macOS only.
- No lazygit fork.
- No automatic merge, rebase, or stash.

### Open questions

None of the product questions currently block implementation. New questions are recorded here as they arise.


---

## 20. Success Criteria

The first production-ready version is successful when:

- a curated collection of dozens or hundreds of repositories loads predictably, and 200 repositories are inspected within 2 seconds;
- explicit repositories and bounded discovery work together without duplicates;
- repositories at different nesting levels are supported;
- unwanted repositories can be excluded;
- displayed Git state agrees with the official Git CLI;
- any available repository can be opened in lazygit with one action;
- the terminal is restored reliably after lazygit exits;
- no background Git process ever prompts in or writes to the terminal;
- visible repositories can be fetched concurrently with understandable progress;
- individual failures do not block the rest of the application;
- installation is possible as one Go binary;
- Git Dashboard does not duplicate lazygit's detailed repository workflows.

---

## 21. Conclusion

Git Dashboard should be implemented as an independent Go TUI rather than a lazygit fork. It has a deliberately narrow responsibility:

- curate and discover local repositories;
- present a cross-repository overview;
- group, filter, and sort repositories;
- run safe batch operations;
- hand detailed work over to lazygit.

The repository inventory combines explicit paths with optional bounded discovery. This supports both deterministic configuration and convenient scanning across heterogeneous directory layouts. Local paths are the primary identity; remote URLs are optional assertions and may later support an explicit clone workflow.

The official Git CLI is the sole Git backend. A typed Go wrapper provides safe command execution and structured parsing without reimplementing Git. This ensures that status, worktrees, authentication, network operations, and lazygit all agree on the same Git behavior.

This architecture minimizes maintenance risk, avoids duplicated Git functionality, and allows the product to grow from a focused dashboard into a reliable repository-family management tool.
