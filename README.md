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
  cmd/atsap-api/              entry point
  internal/ari/               ARI REST client + WebSocket event stream
  internal/ami/                AMI TCP client (login, actions, events)
  internal/config/             env-based configuration
  internal/server/             app's own HTTP server (health checks)
deploy/
  docker-compose.yml           local dev stack
  docker-compose.prod.yml      single-VPS production stack (pulls built images)
  postgres/init/                CDR/CEL schema, applied on first postgres boot
Jenkinsfile                  CI: lint, test, build+push images, deploy
```

`app/` (the interpreter/agent web dashboard) doesn't exist yet — this repo
is currently just the telephony core + control-plane boilerplate.

## Local development

Tooling is pinned via [mise](https://mise.jdx.dev) in `.mise.toml` — the
same versions are used in CI, so "works on my machine" failures are rare.

```bash
mise trust
mise install        # installs Go + golangci-lint at the pinned versions
cp .env.example .env
mise run dev         # docker compose up --build: Asterisk + Postgres + the Go app
```

- Asterisk ARI: http://localhost:8088/ari (basic auth: `voipapp` / `devpassword123`)
- Asterisk AMI: localhost:5038
- App health check: http://localhost:8080/healthz
- SIP: register a softphone to extension `1000` / `devpassword123` at
  `localhost:5060` (UDP) to place a test call — it will be handed to the
  Go app's Stasis application (`voip-app`), which answers it by default
  (see `handleARIEvent` in `api/cmd/atsap-api/main.go`).
- CDR/CEL: every call is logged to Postgres (`cdr` and `cel` tables) by
  Asterisk's `cdr_pgsql`/`cel_pgsql` backends — schema in
  `deploy/postgres/init/`. Inspect with:
  `docker compose -f deploy/docker-compose.yml exec postgres psql -U asterisk -d asterisk`

Other mise tasks:

```bash
mise run test        # go test ./... -race -cover (in api/)
mise run lint         # golangci-lint run ./... (in api/)
mise run build        # build ./bin/atsap-api
mise run dev:logs      # tail the dev stack's logs
mise run dev:down      # stop the dev stack
```

All dev-only credentials (ARI/AMI/SIP passwords) live in
`core/conf/*.conf` and `.env.example` — they must match. **Never
reuse these in production.**

## Production deployment (single VPS via Docker Compose)

Production uses pre-built images (pushed by Jenkins) rather than building
on the server:

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
