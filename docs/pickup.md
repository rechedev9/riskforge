---
summary: "Session pickup template. Rehydrate context when starting work."
read_when:
  - Starting a new work session
  - Resuming after a break
  - Picking up from handoff notes
---

# /pickup

Rehydrate context in <60 seconds when starting work.

1. **Read CLAUDE.md** at project root. Run `scripts/docs-list` to scan docs index.
2. **Repo state**: `git status -sb`; check for uncommitted or unpushed work.
3. **Dependencies**: install if lockfile changed since last session.
4. **Quick gate**: run typecheck + lint to catch anything broken.
5. **Database**: pending migrations?
6. **Dev server**: start if not running; verify endpoints respond.
7. **Last handoff**: check if there are handoff notes from previous session.
8. **Plan next 2-3 actions** as bullets. Execute.

Output: concise bullet summary with any blockers flagged.
