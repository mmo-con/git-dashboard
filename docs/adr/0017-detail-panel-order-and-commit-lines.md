# ADR 0017: Detail panel order and commit lines

Status: Accepted

## Context

[ADR 0013](0013-calmer-table-and-detail-panel.md) ordered the detail panel
as status, errors, other branches, recent commits, remotes, linked worktrees,
and repository, and kept the absolute commit time there.
[ADR 0015](0015-showing-incoming-commits.md) added the incoming commits below
the recent commits, with their author. In use:

- Incoming commits, which belong to the behind count in the status and to
  the next update, stood below the repository's own history.
- Remotes and linked worktrees, which change as rarely as the path, had
  sections of their own between the commits and the repository.
- Recent commits had no author, incoming commits had one, so the two lists
  looked different.
- The absolute time took up to 17 cells, which cut off subjects in a narrow
  panel, while the table already showed the age of the latest commit.

## Decision

The detail panel has these sections, each only when it has content:

1. **Status**, as in ADR 0013. A remote mismatch is a line of the status, in
   the warning color, because it needs attention.
2. **Error**.
3. **Incoming commits**, as defined in ADR 0015.
4. **Recent commits**, or the last commit.
5. **Other branches**.
6. **Repository**: path, family, workspace, and source as in ADR 0013,
   followed by the remotes with their names in the label column and the
   linked worktrees, labeled `linked`.

When the panel is small, lines are dropped as before: older recent commits,
then incoming commits down to the newest, then blank lines, then other
branches and section titles.

Recent and incoming commits use the same line in the detail panel, the
update confirmation, and the operations view: short ID, age as in the
Commit column of the table, author truncated to a fixed width, and subject.
Subjects that do not fit are cut off at the panel edge.

## Consequences

- The panel reads from what needs action to what is static; incoming
  commits stay next to the status they explain.
- Both commit lists line up and leave more room for subjects. The absolute
  commit time is no longer shown; an interactive commit list
  ([#55](https://github.com/mmo-con/headquarters/issues/55)) may show it.
- In a small panel, the remotes are cut off before the path, as part of the
  repository section.
