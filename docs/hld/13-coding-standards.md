<!-- OpenSpec: TRD-HLD-15 -->
# Go Coding Standards

Normative house subset for all Go code in AtsaPBX — human-written or
agent-generated (DECISIONS D-40). Few rules, all machine-checkable where
possible. For anything not covered here, the authorities in order are the
[Go Code Review Comments](https://go.dev/wiki/CodeReviewComments), then the
[Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)
— linked, never copied, so this doc cannot rot against upstream.

Traceability: DECISIONS D-40; TRD Tech stack; TOOLSET.md.

---

## 1. Spec-Driven Development: specifications are law

- OpenSpec delta-spec scenarios, `.proto` contracts, and migration SQL are
  the absolute source of truth. Implement only what they describe.
- Generated code (`.pb.go`, `.connect.go` from `buf`) is NEVER modified by
  hand. `buf breaking` guards the contract.
- Missing spec? STOP and ask for clarification — never invent parameters,
  flows, or signatures.
- Rely on strict types. No `interface{}`/`any` unless the contract defines it.

## 2. Line of sight & error handling

Keep the happy path left-aligned; check errors immediately and return early:

```go
// Good: happy path left-aligned.
func ProcessCall(ctx context.Context, callID string) error {
	if callID == "" {
		return fmt.Errorf("call id is required")
	}
	call, err := getCall(ctx, callID)
	if err != nil {
		return fmt.Errorf("failed to get call: %w", err)
	}
	return routeCall(ctx, call)
}
```

- Errors are always handled, wrapped with `%w`, and returned — and the
  error is always the final return value.
- Error strings are lowercase with no trailing punctuation
  (`fmt.Errorf("failed to route sip call: %w", err)`).
- Never `panic` for normal error handling.
- **Scoped blank-identifier rule:** errors on behavior-affecting I/O must
  never be discarded with `_`. Deferred `Close` calls and writes to
  `httptest` recorders in tests may use `_ =` — anything else needs
  handling or an explicit justification comment.

## 3. Context first

- Every long-running, network, or database operation takes
  `context.Context` as its **first** argument — cancellations, timeouts,
  and hang-ups propagate through it.
- Never store a `Context` in a struct; pass `ctx` as a parameter instead.
- `context.Background()` only where nothing request-scoped exists, with a
  reason — the default is to pass a `Context`.

## 4. Concurrency safety

- Never pass a struct containing a `sync.Mutex` by value — always by
  pointer, or the lock state is copied and deadlocks follow.
- Never launch a goroutine without a documented exit: who stops it, on
  what signal, and what happens to in-flight work.
- The goroutine that creates and writes a channel owns closing it.
- Prefer synchronous functions (results returned directly); callers add
  concurrency where needed — it cannot be removed at the call site.

## 5. Data, names, and defaults

- Prefer zero-value declarations for empty slices/maps
  (`var participants []Participant`), unless a benchmarked capacity or a
  JSON `null`-vs-`[]` distinction requires otherwise.
- Initialisms keep consistent case (`URL`, `ID` → `urlPony`, `callID`,
  never `Url`/`callId`); unexported names are `mixedCaps`.
- Method receivers are one or two letters, consistent per type — never
  `me`/`this`/`self`.
- Ubiquitous language applies to identifiers too: `CallParticipant`, never
  `Leg`, never fixed `Caller`/`Agent` parties (D-17/D-18/D-32, glossary).
- Avoid meaningless package names (`util`, `common`, `misc`).

## 6. Imports & formatting

- `gofmt` clean, always. Tabs for indentation inside `go` code blocks.
- Import grouping, `goimports` style: standard library first, blank line,
  then everything else with `atsap-api` local packages in their own group:

```go
import (
	"context"
	"fmt"

	"github.com/gorilla/websocket"

	"atsap-api/internal/config"
)
```

- No dot-imports except the single sanctioned test exception (circular
  test dependencies); no unused imports.
- Doc comments on all exported names, full sentences starting with the name.

## 7. Machine gates (D-28)

`gofmt`, `go vet`, and `golangci-lint` (including the `depguard` ACL
rule and `goimports` grouping) must pass; `govulncheck` must report no
vulnerabilities. CI fails otherwise — review never re-checks what machines
can check.

## 8. Secure coding rules

Authoritative references, linked never copied (OWASP text is CC BY-SA):
[OWASP Go Secure Coding Practices Guide](https://owasp.org/www-project-go-secure-coding-practices-guide/)
(14 topics, Go examples throughout) and the [gosec rule catalog G1xx–G7xx](https://github.com/securego/gosec/blob/master/RULES.md)
(incl. taint analysis for injection/SSRF/log-injection). House subset,
mapped to this codebase:

1. **Credentials:** env only with fail-fast on missing, never literals
   (G101). Test fixtures use obviously-fake values; any `#nosec G101`
   carries a written justification.
2. **Crypto allowlist:** Argon2id (passwords), Ed25519 (tokens/JWT),
   SHA-256 (key hashes), AES-256-GCM (recordings). No MD5/SHA1/DES/RC4,
   no `InsecureSkipVerify`, TLS ≥1.2 with verified peers (G401–G407).
3. **Network posture:** explicit bind addresses — no `0.0.0.0` in prod
   paths without a documented reason (G102); timeouts on every server
   (`ReadHeaderTimeout` precedent) and context deadlines on every client
   call; no pprof in prod images (G108/G112/G114).
4. **Untrusted input:** all SIP/ARI/AMI data arrives tainted — validate
   and parse strictly at the boundary; never trust channel vars or
   headers for auth decisions (correlation ≠ authentication);
   parameterized SQL only, `$n` placeholders, never `Sprintf`/concat
   (G201/G202/G701).
5. **Logging:** D-39 redaction plus no log injection — sanitize
   externally-sourced strings (newlines, control chars) before they
   reach a log line (G706).
6. **Filesystem:** restrictive creation perms (`0600` files, `0750`
   dirs — G301/G302/G306); no predictable tempfiles; migrations dir
   mounted read-only.
7. **Processes:** no `os/exec` in production code paths (G204);
   `scripts/` tooling excepted and reviewed.
8. **Secrets in structs:** no credential/token/key-hash fields in
   marshaled structs without explicit redaction (G117) — see LLD-02
   §10.2/§10.3 for JWT/`ApiKey` handling.
9. **Suppressions:** `#nosec <RULE> -- justification` is the only allowed
   form; blanket disables forbidden; each re-checked at review, with
   periodic `gosec -nosec=true` runs to audit outstanding suppressions.

Enforcement status: this section is review-enforced today. Machine
enforcement via `gosec` inside the already-pinned `golangci-lint` (zero
new tools) is approved as the implementation follow-up — trialed
read-only against this tree with 5 findings (1 real hardening item, 1
review item, 3 test-hygiene), so the gate is known-green before it lands.

---

## Appendix A — Agent role prompt (reference standard, D-40)

System instruction for AI agents writing Go in this project. It restates
§§1–6 as directives; where it conflicts with the sections above, the
sections win.

```markdown
# Role & Core Objective
You are a principal Go software engineer specializing in
high-performance, real-time VoIP and telephony systems. Your objective is
to implement clean, low-latency, compile-ready Go source code driven
exclusively by the provided specifications (OpenSpec delta specs,
Protobuf/ConnectRPC contracts, migration SQL).

You do not guess architectural needs, invent parameters, or write
open-ended implementations. If a structure or flow is missing from the
specifications, halt and ask for clarification.

# 1. Spec-Driven Development (SDD) Directives
- Specification Is Law: the provided specs are the absolute source of
  truth. Implement only the interfaces and types they describe.
- Compile-Time Enforcement: strict type safety; no `interface{}`/`any`
  unless the contract defines it.
- Implementation Only: deterministic implementations of specified
  behavior. Never hand-edit generated code (`.pb.go`, `.connect.go`).

# 2. Industry Go Coding Standards
Adhere to the [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments)
and the [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md).
Apply the secure-coding rules in §8 (credentials, crypto allowlist, taint
handling, secret redaction); the gosec gate judges this section.

## Control Flow & "Line of Sight"
- Happy path strictly left-aligned; check errors/edges immediately and
  exit early with `return`/`continue`. No nested `if-else`, no deep
  indentation.

## Error Handling
- Errors on behavior-affecting I/O are always handled, wrapped (`%w`),
  and returned as the final return value. Deferred `Close` and test
  `httptest` writes may use `_ =`; anything else needs handling or a
  justification comment.
- Error strings are lowercase, no trailing punctuation
  (e.g. `fmt.Errorf("failed to route sip call: %w", err)`).

## Real-Time Telephony Safety & Performance
- Context First: `context.Context` is the first argument of every
  long-running, network, or database operation.
- Zero-Value Defaults: `var participants []Participant`, not
  pre-allocated empties, unless a benchmark or JSON encoding demands it.
- Mutex Safety: structs holding a `sync.Mutex` travel by pointer only.
- Ubiquitous language in identifiers: `CallParticipant`, never `Leg`.

## Concurrency Guardrails
- Every goroutine has a documented exit (who stops it, on what signal).
- The goroutine that creates/writes a channel owns closing it.

# 3. Markdown Formatting & Presentation Standards
- `go` language tag on code blocks; tabs for indentation; import blocks
  grouped stdlib / blank line / third-party / local `atsap-api`
  (goimports style).
- Inline identifiers in backticks with compiler casing (`CallService`
  exported, `ctx` internal).
- Directory trees in `text` blocks.
```

---

*Decisions: D-40 (adoption). Related: D-24 (API-first), D-28 (machine
gates), D-33 (ConnectRPC), D-35 (toolset).*
