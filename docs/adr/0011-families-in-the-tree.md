# ADR 0011: Families in the tree

Status: Accepted

## Context

The tree view groups repositories by family under collapsible family rows.
[ADR 0004](0004-batch-fetch-and-safe-update.md) defined batch targets for a
flat list: marked repositories, the cursor row, or all visible rows. In the
tree, the cursor can rest on a family row, and repositories of collapsed
families are not displayed. Users want to act on a whole family without
marking each repository, but a batch must not include rows that the user
did not choose. Families themselves are defined by
[ADR 0002](0002-inventory-and-repository-identity.md).

## Decision

A family row stands for its **matching repositories**: the repositories of
the family that pass the active filters, whether or not the family is
expanded. This amends the targets of ADR 0004:

- Without marks, `f` and `u` on a family row target its matching
  repositories, also when the family is collapsed. The user selected the
  family explicitly, so its repositories are not counted as hidden. The
  update preview lists every target before confirmation.
- `Space` on a family row marks its matching repositories, or unmarks them
  if all are already marked.
- In the tree, visible rows are the matching repositories of expanded
  families. `a`, `F`, and `U` therefore exclude collapsed families; rows
  outside the viewport still count.
- Marks survive filter changes and collapsing. Marked targets in collapsed
  families count as hidden in the fetch message and update confirmation, as
  ADR 0004 requires for marks hidden by filters.

Families are listed alphabetically, with the current sort order applied
within each family, and start collapsed. Expansion state is kept when
switching between list and tree. A text or quick filter expands every family
with matches, so matches are never hidden in collapsed families. Expanding or
collapsing during such a filter lasts until it is cleared; clearing it
restores the previous expansion state. The family filter selects families
and does not expand them. Tree navigation keys act only in the tree.

## Consequences

- A family action is a single key and needs no marks, while marks remain the
  way to combine repositories across families.
- `F` and `U` never reach into collapsed families, so collapsing a family
  removes it from "everything visible".
- Unavailable repositories remain family members. Fetch leaves them out;
  the update preview lists them as blocked with their state.
- Concrete keys are listed in [the key binding reference](../keybindings.md).
