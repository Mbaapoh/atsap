## 1. Identity tenant cap (LLD-02 §4a.1, already designed)

- [ ] 1.1 Add the cap input to `CreateTenant` through a port `identity` owns, satisfied at the composition root over `licensing`; verify no new import into `identity` from `licensing` and the `identity-imports-nothing` boundary still passes (design D2, HLD-04 §10.1)
- [ ] 1.2 Refuse at the cap with `FAILED_PRECONDITION` and an entitlement reason (`MaxTenants = 0` disables the check); verify tests cover cap-reached refusal, zero-means-unlimited, and that reads (`ListTenants`, `GetTenant`) are never gated (spec `tenant-provisioning`, AC-06.14)
- [ ] 1.3 Prove grandfathering and isolation untouched: verify lowering the cap removes and hides nothing, and the RLS isolation test passes unchanged in every mode (spec `tenant-provisioning`, D-51)

## 2. PBX extension cap (mirror of D-51)

- [ ] 2.1 Add the extension-cap input to `CreateExtension` through a port `pbx` owns, satisfied at the composition root; verify no new import into `pbx` from `licensing`, and that the port carries a number only — no edition, no capability flags (design D1, D2, D3)
- [ ] 2.2 Refuse the over-cap creation inside the creation transaction with `FAILED_PRECONDITION` and an entitlement reason; verify tests cover the 11th-extension refusal, zero-means-unlimited, and that a racing pair of over-cap creations cannot both commit (spec `extension-management`, design Risks)
- [ ] 2.3 Prove the estate is untouched: verify an over-cap installation keeps all extensions listed, registered and projected, with only new creation refused (spec `extension-management`, AC-06.13)

## 3. Gates and end-to-end proof

- [ ] 3.1 Assert both refusals surface the entitlement reason on the wire with `FAILED_PRECONDITION`, reusing the existing error mapping and never `PERMISSION_DENIED`; verify against the RPC surface (design D5, AC-06.14)
- [ ] 3.2 Run the full gates (`go build ./...`, `go vet ./...`, `go test ./... -race -cover`) green; verify `git diff --stat api/internal/telephony/ api/internal/licensing/` is empty — this change consumes the entitlement, it never alters its publishers (design Goals)
- [ ] 3.3 Keep e2e green under the dev token: verify both suites pass; add a licence fixture for the two-tenant test only if the committed dev token is capped, and do no fixture work otherwise (design D6, D-54)
