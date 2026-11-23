# ADR 0003: Git CLI and process isolation

Status: Accepted

## Context

Git, lazygit, and the dashboard must agree on repository state, while
background processes must not interfere with the terminal or be redirected
to another repository by inherited environment variables.

## Decision

Use only the installed official Git CLI, through the typed client in
`internal/git`; do not use `go-git` or another implementation. The client
owns arguments, working directory, child environment, process isolation,
timeouts, cancellation, output capture, and parsing into domain models. It
does not implement Git transports, authentication, merging, or credentials.

Parse machine-readable output only: porcelain v2, NUL delimiters, and
explicit formats. Do not parse localized or human-oriented output, colors,
column spacing, or stderr. Unknown state must remain unknown, as described
in [ADR 0006](0006-domain-state-and-errors.md).

Every Git process started while the TUI owns the terminal:

- gets `GIT_TERMINAL_PROMPT=0`, `GIT_PAGER=cat`, `--no-pager`, and
  `GIT_EDITOR=true`; read-only commands also get `GIT_OPTIONAL_LOCKS=0`;
- reads `/dev/null`, with stdout and stderr captured;
- starts in a new session without a controlling terminal. SSH and credential
  helpers cannot open `/dev/tty`; ssh-agent, credential helpers, and
  `*_ASKPASS` programs can still work;
- leads its own process group. Cancellation or timeout sends `SIGTERM` to
  the group, then `SIGKILL` after 3 seconds. After Git exits, pipes are
  drained for at most 2 seconds;
- runs without `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`,
  `GIT_COMMON_DIR`, `GIT_OBJECT_DIRECTORY`, and `GIT_NAMESPACE`. Everything
  else, including locale, is inherited.

No shell is involved; paths and URLs are data. Do not change Git
configuration, including `safe.directory`.

## Consequences

Using Git preserves the user's configuration, credential helpers, SSH
agent and `~/.ssh/config`, worktrees, sparse checkouts, and Git features,
instead of risking disagreement between implementations. A background
prompt fails rather than touching the TUI. Terminal handover for interactive
retry is specified in [ADR 0005](0005-terminal-handover-and-job-coordination.md).

### Validation

For each candidate at startup and on a full rebuild:

```text
git -C <path> rev-parse --path-format=absolute --git-dir --git-common-dir --show-toplevel \
    --git-path MERGE_HEAD --git-path rebase-merge --git-path rebase-apply \
    --git-path CHERRY_PICK_HEAD --git-path REVERT_HEAD --git-path BISECT_LOG \
    --git-path FETCH_HEAD
```

Only paths with a `.git` entry are passed to Git. Other paths are classified
through the filesystem, including bare repositories with `HEAD`, `objects`,
and `refs`, because Git refuses `-C` access to bare repositories with
`safe.bareRepository = explicit`. Cache the worktree root, Git and common
directories, and marker paths. `--git-path` resolves worktree-specific paths;
`rev-parse` has no NUL mode, so reject paths containing newlines.

### Inspection

The normal inspection uses three processes per entry:

```text
git --no-pager -C <root> status --porcelain=v2 --branch --show-stash -z --untracked-files=<normal|no>
git --no-pager -C <root> for-each-ref --format=<branch format> refs/heads [<upstream patterns>]
git --no-pager -C <root> config -z --get-regexp '^remote\..*\.url$'
```

The branch format is:

```text
%(HEAD)%00%(refname)%00%(upstream:short)%00%(upstream)%00%(upstream:track,nobracket)%00%(worktreepath)%00%(objectname)%00%(objectname:short)%00%(authorname)%00%(committerdate:unix)%00%(subject)
```

`status` supplies branch, HEAD, upstream, ahead/behind, entries, and, since
Git 2.35, stash count. `for-each-ref` supplies local branches, tracking
state, checkout worktree, and latest commit. Tracking state is untranslated:
empty, `gone`, `ahead N`, `behind N`, or `ahead N, behind M`. Its `*` line
is HEAD. With
`[branches].enabled = false`, limit it to the checked-out branch and skip it
for unborn or detached HEAD.

Upstream patterns select the short upstream from status using
`refs/remotes/<upstream>`, `refs/<upstream>`, and, when listing only the
checked-out branch, `refs/heads/<upstream>`. The full refname matching the
`*` line's `%(upstream)` supplies the upstream commit for safe update.
If the branch or upstream changed between calls, or no line matches, the
upstream commit remains unknown.

Detached HEAD or a branch switched between calls needs a fourth process:

```text
git log -1 --no-color --format=%H%x00%h%x00%an%x00%ct%x00%s
```

Git 2.34 lacks the porcelain stash header and needs one additional process:
`git rev-list --walk-reflogs --count --ignore-missing refs/stash`. Stashes
belong to the store; all its worktrees report the same count. The dashboard
only reports them; applying or dropping them stays with lazygit. `config`
exits with 1 when no remote exists.

Operation markers and last fetch time come from `stat` on cached paths. An
entry's last fetch time is the newest `FETCH_HEAD` among its store's
entries, so fetches by other tools count.

Recent commits are loaded only for the selected detail panel, when
`[ui].detail_commits` exceeds 1:

```text
git log --max-count=<n> --no-color --no-show-signature --format=%H%x00%h%x00%an%x00%ct%x00%s HEAD --
```

Cache the result by repository and HEAD; reload after HEAD moves or after
failure and refresh. Cancel a request when selection moves on, so scrolling
runs at most one such process at a time.

### Parsing

- `# branch.oid (initial)` means unborn, `# branch.head (detached)` means
  detached, absence of `# branch.upstream` means no upstream, and an
  upstream without `# branch.ab` means the upstream is gone. Ignore unknown
  headers.
- Parse entry types `1`, `2`, `u`, `?`, and `!`. An unknown type makes the
  worktree state unknown.
- Staged counts type `1`/`2` entries with index status other than `.`.
  Modified counts worktree `M` or `T`; deleted counts worktree `D` (staged
  deletions count as staged); renamed counts type `2` in addition to
  staged; conflicts count type `u`; untracked counts type `?`.

Fetch and update commands and their safety constraints are specified in
[ADR 0004](0004-batch-fetch-and-safe-update.md).
