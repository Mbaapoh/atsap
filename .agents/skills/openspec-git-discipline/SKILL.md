---
name: openspec-git-discipline
description: Enforce git commit discipline for OpenSpec-driven work. Use before any commit, or when asked to commit, review staged changes, or push.
---

Commits are the audit trail the D-log points at. Small, scoped, truthful —
and never containing secrets.

**Lens:** the DevOps engineer guarding the audit trail — small, truthful,
secret-free, reversible.

## Before staging

1. Inspect `git status`, `git diff`, and `git log --oneline -10`. Understand
   what changed and what the recent message style is.
2. Stage only intended files. House history style: `docs: ...` for
   documentation, `Add ...` for new artefacts. One change per commit —
   a docs edit and a code edit never share a commit unless the change
   proposal says they are one unit.
3. Scan the staged diff for secrets: tokens, keys, passwords, connection
   strings, PII, audio. `.env` is never staged (repo `.gitignore` covers
   it — verify, don't assume).

## Message rules

- First line: imperative scope + subject (`docs: add portal architecture
  and logging standards`), then bullet body for multi-part commits,
  then `Requirements traceability:` / D-number refs where the repo
  convention uses them.
- No `Co-authored-by`, `Agent`, `AI`, or attribution tags unless the user
  explicitly requests them. Author is the logged-in git user.
- Describe what IS in the commit, verified from the diff — never from
  memory of what was "supposed" to change.

## OpenSpec alignment

- Commit change artefacts (proposal/design/tasks/delta specs) as units per
  the change; archive commits move them to `openspec/specs/` living specs.
- D-28: no change merges the day it was generated. A commit is not a merge
  — but flag same-day generated→merged sequences for human judgement.
- Untracked `openspec/changes/<name>/` work-in-progress is left alone unless
  the change owner says to include it.

## Push rules

- Commit and push are separate authorizations. A request to commit is not
  permission to push; ask, or wait for an explicit push instruction.
- Never `--force`, never `--no-verify`, never amend someone else's commit.
  If hooks reject, fix the cause and commit fresh.
