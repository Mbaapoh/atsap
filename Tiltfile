# Tilt orchestration for atsap local development.
#
# Industry split, mirroring the contract in .env.example:
#   - deploy/docker-compose.yml owns TOPOLOGY (services, networks, ports).
#   - This file owns ORCHESTRATION (build strategy, startup order, reload).
#   - api/Dockerfile[.dev] owns IMAGES (prod stays distroless; .dev keeps
#     the toolchain for live_update). Never point compose at .dev directly.
#
# Usage:
#   tilt up                    # full stack with live reload (mise run tilt:up)
#   tilt up -- --no-sipua      # app-only iteration, no sip-ua sidecar
#   tilt up -- --no-console    # backend only, no console dev server
#   tilt down                  # stop everything (mise run tilt:down)
#
# While Tilt runs, edits are picked up without image rebuilds:
#   - Go (api/cmd, api/internal): synced, recompiled in the container,
#     restarted — seconds.
#   - Protos (api/proto): Go and console stubs regenerated, which in turn
#     live-updates the app and hot-reloads the console.
#   - Console (console/src): Vite hot-reloads the browser in place.
# Do not run `make dev-app` or `make dev` while Tilt is up: they rebuild the
# production image through compose and replace Tilt's container behind its
# back (both refuse while Tilt is running).

version_settings(constraint='>=0.37.0')
analytics_settings(False)
update_settings(max_parallel_updates=3)

config.define_bool(
    'no-sipua',
    args=True,
    usage='Skip the sip-ua sidecar (faster app-only iteration).',
)
config.define_bool(
    'no-console',
    usage='Skip the console dev server (backend-only iteration).',
)
cfg = config.parse()

# Single source of truth for topology; the docker_build stanzas below
# override the compose `build:` sections by matching `image:` name.
docker_compose('./deploy/docker-compose.yml')

# ---------------------------------------------------------------- builds ---

# Asterisk is built from source (several minutes) and its version is pinned
# (see core/Dockerfile: never downgrade below 22.11.0). Watch ONLY the files
# that can legitimately change it, so a Go edit never triggers a rebuild.
# NOTE: `only` paths are relative to the build context (./core), NOT to the
# Tiltfile — root-relative patterns would empty the Docker build context and
# break `COPY conf/` (seen 2026-10-06: "/conf": not found).
docker_build(
    'atsap-core:dev',
    context='./core',
    dockerfile='./core/Dockerfile',
    only=[
        './Dockerfile',
        './conf/',
    ],
)

# Go control-plane: toolchain-rich dev image + in-container recompile.
# A save under api/ resyncs sources, rebuilds /out/app, and restarts the
# container in seconds — no image rebuild, Asterisk untouched.
# NOTE: `only` is context-relative (see asterisk note above); `sync`/`run`
# triggers stay Tiltfile-relative.
_GO_ONLY = [
    './Dockerfile.dev',
    './go.mod',
    './go.sum',
    './cmd/',
    './internal/',
    './migrations/',
]

# The dev licence public key (make dev-licence, pbx-call-placement D8) is
# a BUILD arg, so it is read from .env here; compose's env_file only
# reaches the container at runtime. Empty when dev-licence has not run.
def _dotenv(key):
    for line in str(read_file('.env', '')).splitlines():
        if line.startswith(key + '='):
            return line[len(key) + 1:].strip()
    return ''

docker_build(
    'atsap-api:dev',
    context='./api',
    dockerfile='./api/Dockerfile.dev',
    build_args={'DEV_LICENCE_PUBKEY': _dotenv('ATSAPBX_DEV_LICENCE_PUBKEY')},
    only=_GO_ONLY,
    live_update=[
        sync('./api/cmd', '/src/cmd'),
        sync('./api/internal', '/src/internal'),
        sync('./api/migrations', '/src/migrations'),
        sync('./api/go.mod', '/src/go.mod'),
        sync('./api/go.sum', '/src/go.sum'),
        run(
            # Same ldflags the image was built with, or a save would silently
            # drop the dev licence key and the rig would fall back to Setup.
            'cd /src && CGO_ENABLED=0 go build -trimpath -ldflags="$DEV_LICENCE_LDFLAGS" -o /out/app ./cmd/atsap-api',
            trigger=['./api/cmd', './api/internal', './api/go.mod', './api/go.sum'],
        ),
        restart_container(),
    ],
)

docker_build(
    'atsap-sipua:dev',
    context='./api',
    dockerfile='./api/Dockerfile.dev',
    build_args={'CMD_PATH': './cmd/sip-ua'},
    only=_GO_ONLY,
    live_update=[
        sync('./api/cmd', '/src/cmd'),
        sync('./api/internal', '/src/internal'),
        # sip-ua never reads migrations, but Dockerfile.dev COPYs them and
        # they are in _GO_ONLY: without this sync a migration edit falls
        # outside live_update and forces a full sip-ua image rebuild.
        sync('./api/migrations', '/src/migrations'),
        sync('./api/go.mod', '/src/go.mod'),
        sync('./api/go.sum', '/src/go.sum'),
        run(
            'cd /src && CGO_ENABLED=0 go build -trimpath -o /out/app ./cmd/sip-ua',
            trigger=['./api/cmd', './api/internal', './api/go.mod', './api/go.sum'],
        ),
        restart_container(),
    ],
)

# -------------------------------------------------------------- resources ---

# Fail fast with a human message when the 12-factor env contract is missing
# (compose would otherwise start `app` with empty config).
local_resource(
    'env-guard',
    cmd='test -f .env || (echo "FATAL: missing .env — run: cp .env.example .env" >&2 && exit 1)',
    deps=['.env.example'],
    labels=['preflight'],
)

dc_resource('postgres', labels=['infra'])

dc_resource(
    'nats',
    links=['http://localhost:8222'],
    labels=['infra'],
)

# Captures dev mail (password resets, invitations); its web UI lists them.
dc_resource(
    'mailpit',
    links=['http://localhost:8025'],
    labels=['infra'],
)

dc_resource(
    'asterisk',
    resource_deps=['postgres'],
    # Manual trigger: a source build costs minutes; rebuild explicitly from
    # the Tilt UI after touching core/. The first `tilt up` still builds it.
    trigger_mode=TRIGGER_MODE_MANUAL,
    links=['http://localhost:8088'],
    labels=['voice'],
)

dc_resource(
    'app',
    resource_deps=['postgres', 'nats', 'asterisk', 'mailpit', 'env-guard'],
    links=['http://localhost:8080/healthz'],
    labels=['app'],
)

dc_resource(
    'sipua',
    resource_deps=['asterisk'],
    auto_init=not cfg.get('no-sipua', False),
    labels=['voice'],
)

# ------------------------------------------------------- code generation ---

# A .proto edit regenerates both sides from the same source (D-68): the Go
# stubs (api/internal/genproto, which live_update then compiles into the
# running app) and the console's TypeScript (console/src/api/gen, which
# Vite then hot-reloads). Not run at startup: the generated code is
# committed, and buf's remote Go plugins need the network.
local_resource(
    'proto-gen',
    cmd='cd api && buf generate && cd ../console && mise exec -- npm run gen',
    deps=['./api/proto', './api/buf.gen.yaml', './console/buf.gen.yaml'],
    auto_init=False,
    resource_deps=['console-deps'],
    labels=['codegen'],
)

# ---------------------------------------------------------------- console ---

# The console's locked dependencies, reinstalled only when the lockfile
# changes (D-68: `npm ci`, never an implicit upgrade).
local_resource(
    'console-deps',
    cmd='cd console && mise exec -- npm ci --no-audit --no-fund',
    deps=['./console/package-lock.json', './console/mise.toml'],
    labels=['console'],
)

# Vite's dev server: hot module reload in the browser on every save under
# console/src, and /atsapbx.v1.* proxied to the app (D-69). Playwright
# reuses this server when it is up. No container, no build.
local_resource(
    'console',
    serve_cmd='cd console && exec mise exec -- npm run dev',
    resource_deps=['console-deps', 'app'],
    readiness_probe=probe(http_get=http_get_action(port=5173, path='/'), period_secs=2),
    links=['http://localhost:5173'],
    auto_init=not cfg.get('no-console', False),
    labels=['console'],
)

