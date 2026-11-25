# Architecture decision records

ADRs record the project's architectural decisions, their context,
consequences, and constraints. They are the authoritative location for these
decisions; [the architecture overview](../architecture.md) links to them.
Configuration and key references describe their user-facing interfaces,
not the reasons for the decisions.

The initial records were reconstructed from the existing architecture
document. They describe accepted decisions, not a contemporaneous record of
when each decision was made. Alternatives are included only where the
original document recorded them.

Use sequential numbers and descriptive names. Each record has a status,
context, decision, and consequences. Correct an existing record when it
misstates the decision; when a decision changes, add a new record and link
the two. If the new record replaces the whole decision, mark the old one
superseded; a superseded record remains as history, not current guidance. If
it changes only part of the decision, mark the old one amended and point to
the new record at each changed passage; the rest of the old record remains
current. Update the index and affected documentation when adding,
amending, or superseding a record.

| Status | Meaning |
|---|---|
| Accepted | The decision is current. |
| Accepted, amended by ADR NNNN | The decision is current except for the passages that the linked ADR changes. |
| Superseded | A later, linked ADR replaces the decision. |
