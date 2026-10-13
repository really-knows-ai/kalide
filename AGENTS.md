# AGENTS.md — kalide

Rules for every agent working in this repo (coordinator, planners, reviewers,
spec-writers, implementers). Keep this file short; add only rules that prevent
a real failure.

## apg plan notes are broken — never use them

In this apg version, plan notes (`apg_plan_note`, `apg plan note`) are broken.
A plan note is stored in the graph with a project-prefixed FQN such as
`typed-records/plan.note-1`. Every durable spec write (`apg_node` / `apg_edge`
add, update, rm) validates all Note nodes against the spec FQN form
`<layer>.<type>.<name>` and refuses with:

    apg: Note `typed-records/plan.note-1`: `typed-records/plan.note-1` is not a
    node FQN of the form <layer>.<type>.<name>

Once a single plan note exists, **every spec mutation in the worktree is
refused**, whatever node it targets.

Rules, for every agent and every role:

- NEVER call `apg_plan_note`, and never create a plan Note by any other route.
- Record a deviation, concern, decision or commit hash in your returned report
  to the coordinator. The coordinator carries it into the final human-gate
  summary. Do not store it in the plan.
- `apg_plan_done` is fine and is still how a task is marked done. Call it with
  `directory=` set to the worktree.
- If a plan note already exists (an `apg_query` for
  `MATCH (n:Note) WHERE n.fqn STARTS WITH '<project>/' RETURN count(*)` is
  non-zero) and spec writes are refused with the message above, stop and tell
  the coordinator. Do not work around it.

Recovery (coordinator/builder only): delete every `"type":"note"` record and
every `"type":"details"` record whose `from` contains `.note-` from
`apg/.trans/plans/<project>.jsonl` in the worktree, back the file up first,
then run `apg_scan` on the worktree. Confirm the `MATCH (n:Note)` count above
is 0. Plan files are transient and gitignored, so this changes no commit.

## Other conventions

- Every `apg_*` plan and session call for a worktree passes `directory=` set to
  that worktree path. Without it the tool defaults to the main checkout, where
  the plan does not exist.
- A leftover `apg session` blocks the next one. If `apg_session start` reports
  a live session and no agent owns it, the coordinator aborts it
  (`apg_session action=abort`).
- Never push, tag or release from an agent. Merge only with `apg project merge`
  from the main checkout, after the human gate.
