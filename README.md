# atsap

A Go control-plane service on top of Asterisk. The Go app drives calls via
the **Asterisk REST Interface (ARI)** — answering, playing media,
originating calls, bridging — and observes system/call events via the
**Asterisk Manager Interface (AMI)**. Asterisk's dialplan just hands every
inbound call to a Stasis application; all call logic lives in Go.

## Layout

```
core/                        Asterisk (built from source) + dialplan/config, image: atsap-core
  conf/                       dev config baked into the image
  conf.prod/                  production config overlay (see its README)
api/                          Go control-plane, image: atsap-api
  cmd/atsap-api/              entry point (composes; call logic lives in the app layers)
  cmd/ari-playground/         dev tool for poking ARI/AMI live (not shipped)
  internal/
    shared/                   shared kernel across bounded contexts
      domain/ event/ apperrors/ context/   (landing with telephony-core)
    telephony/                bounded context: telephony-core
      domain/ ports/ application/          (landing with telephony-core)
      acl/                    Asterisk Anti-Corruption Layer
        ari/                  ARI REST client + WebSocket event stream
        ami/                  AMI TCP client (login, actions, events)
    identity/                 bounded context: identity (Tier 0)
      domain/ ports/ application/ postgres/
                              tenants, principals, Argon2id credentials,
                              Ed25519 tokens, RBAC, append-only audit log
    pbx/                      bounded context: pbx-core (Tier 1, in progress)
      domain/                 extensions, validation, SIP credentials
      acl/asterisk/           PJSIP Realtime projection (D-47) — the only
                              package that names ps_endpoints/auths/aors
    postgres/                 pgxpool wiring, RLS tenant-context helper (with telephony-core)
    nats/                     JetStream publisher (with telephony-core)
    config/                   env-based configuration
    server/                   app's own HTTP server (health checks)
deploy/
  docker-compose.yml           local dev stack (Asterisk + Postgres + NATS + app)
  docker-compose.prod.yml      single-VPS production stack (pulls built images)
  postgres/init/               CDR/CEL + atsapbx database provisioning
scripts/check-docs.sh          doc wiring checks (HLD markers, decision refs, links)
scripts/check-diagrams.sh      renders every mermaid block in docs/ (mise run diagrams)
Jenkinsfile                  CI: lint, test, vuln-scan, docs, build+scan+push, deploy
```

**How work moves through this repo — and how to resume an in-flight
change — is [`docs/WORKFLOW.md`](docs/WORKFLOW.md).** Start there after a
break: it names the document chain, the propose → apply → archive loop,
the gates, and which agent does which work.

`portal/` (the React + TypeScript web dashboard, D-36) doesn't exist yet. Two
bounded contexts are implemented so far, in the build order fixed by
[`docs/hld/04-bounded-contexts.md` §10](docs/hld/04-bounded-contexts.md):
`telephony-core` (LLD-01 — originate, bridge, hangup, proven end to end
against real Asterisk) and `identity` (LLD-02 — tenants, principals,
credentials, RBAC, audit). Authentication is built but **not yet enforced
on RPCs**: that cutover is its own later change, so the e2e suite and the
UAT rig still call the API without tokens.

`golangci-lint`'s `depguard` rule and `internal/archtest` both enforce
that nothing outside `internal/telephony/acl` may import the ARI/AMI
adapters (HLD 01-architecture.md §1.2).

## Local development

Tooling is pinned via [mise](https://mise.jdx.dev) in `.mise.toml` — the
same versions are used in CI, so "works on my machine" failures are rare.

```bash
mise trust
mise install        # installs Go + golangci-lint at the pinned versions
cp .env.example .env
mise run dev         # docker compose up --build: Asterisk + Postgres + the Go app
```

**Always run Go commands through mise** (`mise exec -- go ...` / `mise run test` /
etc.), not a bare `go` on your `$PATH`. `go.mod`'s `go 1.25` directive makes a
bare `go` on an older version try to download the matching toolchain over the
network, which can hang for minutes — `mise exec --` puts the pinned version
first instead.

- Asterisk ARI: http://localhost:8088/ari (basic auth: `voipapp` / `devpassword123`)
- Asterisk AMI: localhost:5038
- App health check: http://localhost:8080/healthz
- SIP: two static PJSIP dev-test endpoints, `1000` and `1001`
  (both `devpassword123` at `localhost:5060` UDP), exist as fixtures for
  the LLD-01 walking-skeleton test (`core/conf/pjsip.conf`). They are not
  dialed into directly — calls are originated by the Go app itself via
  `telephony/application.Service.InitiateCall`, which drives Asterisk
  through the ACL (`api/internal/telephony/acl`); there is no
  auto-answer, and no `InitiateCall` RPC is exposed yet (`GetCall` is
  the only ConnectRPC method in this change).
- CDR/CEL: every call is logged to Postgres (`cdr` and `cel` tables) by
  Asterisk's `cdr_pgsql`/`cel_pgsql` backends — schema in
  `deploy/postgres/init/`. Inspect with:
  `docker compose -f deploy/docker-compose.yml exec postgres psql -U asterisk -d asterisk`
- AtsaPBX app database (`atsapbx`): a separate database on the same
  Postgres container (`deploy/postgres/init/02-atsapbx.sql`), reachable
  from the host at `localhost:15432` for the `api/` integration test
  suite (`docs/TESTING.md`), e.g.
  `DATABASE_URL=postgres://atsapbx_app:devpassword123@localhost:15432/atsapbx?sslmode=disable mise exec -- go test -tags integration -p 1 ./... -race` (run from `api/`; `-p 1` is required once more than one package's integration tests share the dev database — see `docs/TESTING.md` §1)

Other mise tasks:

```bash
mise run test        # go test ./... -race -cover (in api/)
mise run lint         # golangci-lint run ./... (in api/)
mise run vuln         # govulncheck (pinned) on the Go module
mise run docs         # doc wiring checks (HLD markers, decision refs, links)
mise run ci           # full local gate in pipeline order: docs, lint, vuln, test
mise run build        # build ./bin/atsap-api
mise run dev:logs      # tail the dev stack's logs
mise run dev:down      # stop the dev stack
mise run dev:app       # rebuild + restart only the app service (fast)
mise run uat:auto      # automated UAT on real baresip phones (signaling + media, rig restored after)
```

All dev-only credentials (ARI/AMI/SIP passwords) live in
`core/conf/*.conf` and `.env.example` — they must match. **Never
reuse these in production.**

## Git workflow and branching

`main` is the source of truth and always deployable; `develop` is the
integration branch where tested work accumulates. Feature, bugfix, hotfix
and release branches branch off `develop` (or `main` for hotfixes), are
merged back to `develop` once reviewed and green, and `develop` is promoted
to `main` only after it has passed the full CI and walking-skeleton test
suite (D-28: no change merges the day it was generated).

## Production deployment (single VPS via Docker Compose)

Production uses pre-built images (pushed by CI) rather than building
on the server:

> **Scope today:** this single-VPS Compose flow is the current deployment.
> The richer topology in `docs/hld/06-deployment.md` — Ansible roles,
> Traefik ingress, MinIO/S3, the `atsapbx-support-bundle` tool and its
> canary/expand-contract rollout — is R1.0 EPIC-09 roadmap work, not yet
> implemented. Postgres for the app's own `atsapbx` database and NATS
> JetStream join this Compose stack with the telephony-core walking
> skeleton (`openspec/changes/telephony-core-originate-bridge-hangup/`).

```bash
# on the VPS, one-time:
git clone <this repo> /opt/atsap
cd /opt/atsap
cp .env.example .env            # fill in real, unique secrets
mkdir -p core/conf.prod
cp core/conf/*.conf core/conf.prod/
# edit core/conf.prod/*.conf: real SIP trunks, strong ARI/AMI
# passwords, restrict manager.conf permit/deny to the app container only

REGISTRY=registry.example.com/atsap IMAGE_TAG=latest \
  docker compose -f deploy/docker-compose.prod.yml up -d
```

Jenkins (see `Jenkinsfile`) handles subsequent deploys: on `main`, it
builds both images, pushes them to `REGISTRY`, then SSHes into the VPS to
`git pull`, `docker compose pull`, and `docker compose up -d`.

Networking note: Asterisk needs its SIP and RTP ports reachable from the
public internet (UDP 5060 + the RTP range, 10000-10200 by default). On
cloud VPS providers behind NAT, you'll also need to configure PJSIP's
`external_media_address`/`external_signaling_address` in
`pjsip.conf` — not needed for local dev, required for real-world SIP
trunks.

### Configuring Jenkins

1. Add credentials:
   - `docker-registry-creds` — username/password with push access to your registry.
   - `atsap-deploy-ssh-key` — SSH private key for the deploy user on the VPS.
2. Set job/environment variables: `REGISTRY`, `DEPLOY_HOST`, `DEPLOY_USER`, `DEPLOY_PATH`.
3. Use a multibranch pipeline (or adjust the `when { branch 'main' }` gates
   in `Jenkinsfile` to match your branching model) pointed at this repo's `Jenkinsfile`.

The agent needs Docker available (socket access) and `curl`; Go and
golangci-lint are installed on demand by mise, so the Jenkins agent itself
doesn't need them pre-installed.
