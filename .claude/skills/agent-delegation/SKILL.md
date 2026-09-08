---
name: agent-delegation
description: Decide which agent does a piece of work — Claude for judgement, OpenCode with a cheaper model for mechanical work. Use when planning a change, before starting a long task, or when about to spend a lot of tokens on something repetitive.
---

Two agents work this repository and they are not interchangeable. Claude
Code holds the judgement; OpenCode with a cheaper model holds the volume.
Sending judgement work to the cheap model produces confident wrong
architecture; sending mechanical work to the expensive model burns budget
for nothing.

**Lens:** the engineering manager staffing a task — right person, right
job, and the reviewer is never the same person as the doer.

## Where this sits

This is **project policy**, and it outranks the generic routing default in the
`dispatcher` skill, which says to delegate implementation broadly. The
dispatcher's §0 precedence defers to a project's own delegation policy for
exactly this reason: a project knows which of its work is safe to hand out and
a general orchestrator does not.

So when the dispatcher would route a task to a coder and this file says keep
it, it is kept — and the task's completion note records why, rather than the
decision being re-argued each time.

## The rule

**Claude decides, OpenCode executes; Claude verifies what comes back.**

Delegation is never a transfer of responsibility. Work that comes back
from OpenCode is unreviewed work: it is checked against the spec and the
gates before it counts as done. On this project that check has already
caught defects three separate times — a zero `created_at`, a service
missing from a generated OpenAPI document, and a token claim that was
carried but never authorized. In each case OpenCode reported success.

## Claude keeps (high judgement)

- Anything that ends in a `docs/DECISIONS.md` entry, or should.
- HLD and LLD authoring; bounded-context and dependency-direction calls.
- OpenSpec `proposal.md`, delta `specs/`, and `design.md` — what the
  system must do, and why this approach over the alternative.
- Security design: authorization, credential handling, tenant isolation,
  anything touching INV-01/03/10/11.
- Any empirical spike whose outcome settles a decision (the D-47
  Realtime test, the `md5_cred` test) — the value is in choosing what to
  test and reading the result honestly, not in typing the commands.
- Reviewing anything OpenCode returns.
- Deciding that a task is mechanical enough to delegate. That decision is
  itself judgement, and it is made here.

## OpenCode takes (mechanical, high volume)

- Docker image rebuilds, container restarts, and the slow verification
  loops around them.
- Running an existing test suite and reporting failures verbatim.
- Repetitive edits with a fixed, stated shape: renaming across many
  files, applying one lint fix repeatedly, filling a table from a
  known-good template.
- Generating code from a schema that already exists (`buf generate`),
  and re-running it after a proto edit.
- Scaffolding boilerplate whose shape is already fixed by an existing
  example in the repo — a store following the pattern of another store,
  a test file following the pattern of another test file.
- Long UAT rigs: driving baresip, capturing SIP traces, collecting logs.

## Never delegate

- A task whose spec is ambiguous. Resolve the ambiguity first, or the
  cheap model will resolve it for you, silently and probably wrongly.
- A task where "done" cannot be checked by a machine or by reading a
  diff. If the only verification is judgement, the judgement agent does
  the work.
- The first instance of a new pattern. Delegate the tenth store, never
  the first — the first one *is* the design.
- Anything touching credentials, grants, or the bypass paths.

## How to delegate well

1. **Write the prompt against the spec, not against your memory.** Point
   at the change folder, the LLD section, and the exact task numbers. A
   prompt that paraphrases the requirement invites drift from it.
2. **State the verification in the prompt** — but know who can run it.
   OpenCode sandboxes paths outside the project, and `mise`-managed
   toolchains live in `$HOME`, so `mise exec -- go ...` is silently
   rejected (`permission requested: external_directory ...;
   auto-rejecting`) and the run **still exits 0**. Give absolute binary
   paths instead — `/home/<user>/.local/share/mise/installs/go/<ver>/bin/go`
   is permitted and tested. Where the agent cannot verify, say so in the
   brief and have it report what it wrote rather than claiming a pass.
   **`opencode run`'s exit code is not an acceptance signal**: it returns
   0 after a rejected permission with nothing run. Read the output.
3. **Name what must not change.** Boundary rules are invisible to a model
   that has not read the HLD: say "do not edit anything under
   `internal/telephony/`", do not assume it.
4. **Ask for the output verbatim.** Summarised test results hide
   failures. Require the actual command output.
5. **Verify independently before accepting.** Re-run the gates yourself:
   `mise run docs`, `mise run lint`, `mise run proto`, `mise run test`.
   Read the diff. Do not accept a green report as evidence of green.
6. **Record what was delegated** in the task's completion note, so the
   audit trail says who did what.

## Cost sense, without false economy

Delegating is cheaper per token and more expensive per mistake. The break
-even is roughly: if verifying the result costs as much thought as doing
the work, do the work. Rebuilding a Docker image and watching it boot is
far below that line. Deciding what a bounded context may import is far
above it.

Budget pressure is a reason to delegate more mechanical work. It is never
a reason to delegate judgement, skip the verification step, or accept an
unread diff.
