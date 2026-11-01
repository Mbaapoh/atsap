# Agents — start here

You are continuing AtsaPBX, a multi-tenant PBX and contact-centre
platform built on Asterisk (Go API in `api/`, React console in
`console/`, engine config in `core/`).

1. Read **[docs/HANDOFF.md](docs/HANDOFF.md)**. It says what is next (in
   order), how the work is run, the commands for every test tier, and the
   traps on the dev rig.
2. Then read **[docs/PROGRESS.md](docs/PROGRESS.md)** (the tracker) and,
   before proposing an approach, **[docs/DECISIONS.md](docs/DECISIONS.md)**.
   Do not re-litigate a recorded decision.

Non-negotiables:
- **One OpenSpec change per piece of work**, and one commit per task.
- **Every test tier green before a task is ticked.**
- **Go integration tests run only on the `atsapbx_test` database**, never
  the rig's live one.
- **Ask the owner before stopping their phones**, adding a dependency, or
  taking a security trade-off.
