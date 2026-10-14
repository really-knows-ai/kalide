# AGENTS.md — kalide

Rules for every agent working in this repo (coordinator, planners, reviewers,
spec-writers, implementers).

## Core refactoring principles

Make the smallest change that fully solves the task. “Lazy” means avoiding
unnecessary code—not skipping the work needed to make the change correct.

- Understand the full scope first: read the code being changed and trace
  affected callers, tests, fixtures, configuration, and exports. Consider what
  could break for users.
- Prefer the simplest complete option: skip what isn’t needed; reuse project
  code and patterns; use the standard library or platform features; use an
  already-installed dependency; then write the minimum new code that works.
- Keep the diff lean. Avoid speculative features, abstractions, wrappers,
  options, configuration, and “for later” code. Prefer deleting code when that
  solves the problem. Follow the project’s existing structure and conventions.
- Don’t optimize for line count alone; a few clear lines are simpler than a
  short, hard-to-understand line.
- Fix bugs at their shared source. Check a function’s callers before changing
  it, and fix the root cause once rather than patching individual call sites.
- Preserve safeguards when moving or merging code, especially validation and
  error handling that protect against data loss.
- Choose correctness over cleverness. When similarly small approaches differ,
  choose the one that handles edge cases correctly.
- Test meaningful new logic. Branches, loops, parsers, money or security logic,
  and new scripts or apps need one small test or assert-based check. Trivial
  changes don’t need tests.
- Document intentional shortcuts with a comment in this form:
  `shortcut: <the limit>, <when to upgrade>`.
- Never cut trust-boundary validation, data-loss protections, security,
  accessibility, required hardware calibration, or anything the user
  explicitly asked for.

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
- Stale graph: scan from the worktree ROOT (`apg_scan directory=<worktree>`),
  never from inside its `apg/` subdirectory (that scopes the scan to
  `<worktree>/apg`, has no Go frontend, and fails with `spec drift: code FQN …
  is gone`). If planned nodes stay "not realized" although the code exists
  (empty `path` on a Function in the graph, `apg_plan_verify` blocked), a splice
  scan will not repair it: delete the worktree's `apg/.trans/db.lbug` and run a
  full `apg_scan` from the worktree root. Check with
  `MATCH (f:Function) WHERE f.path = '' RETURN f.fqn`; only nodes of unbuilt
  `creates` tasks should remain.
- Never push, tag or release from an agent. Merge only with `apg project merge`
  from the main checkout, after the human gate.
