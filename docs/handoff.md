---
summary: "Session handoff template. Dump state so the next session can resume fast."
read_when:
  - Ending a work session
  - Switching context
  - Handing off to another agent
---

# /handoff

Dump current state so the next session resumes in <30 seconds.

Include (in order):

1. **Scope/status**: what you were doing, what's done, what's pending, blockers.
2. **Working tree**: `git status -sb`; any local commits not pushed?
3. **Branch/PR**: current branch, PR number if any, CI status.
4. **Dev server**: running? which ports?
5. **Tests**: which passed, which failed, what still needs to run. Copy specific errors.
6. **Database**: pending migrations or schema changes not yet generated?
7. **Next steps**: ordered bullets, most important first.
8. **Risks/gotchas**: flaky tests, env vars, feature flags, brittle areas.

Output: concise bullet list. Include copy-paste commands for anything running.
