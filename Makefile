# AtsaPBX — single entrypoint for local workflows.
#
# Invoke via mise so recipes resolve the PINNED toolchain (.mise.toml):
#   mise run <task>          # thin wrappers, same names as the targets below
#   mise exec -- make <target>
# Bare `make` works only if go/tilt/docker/buf/golangci-lint are already on
# PATH at the right versions — supported path is mise (README, D-60).
#
# Split of responsibilities (D-60):
#   deploy/docker-compose.yml owns TOPOLOGY (services, networks, ports),
#   Tiltfile owns ORCHESTRATION (build strategy, startup order, reload),
#   this file owns WORKFLOWS (the commands humans and CI type).

MAKEFLAGS += --no-print-directory
SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c

REGISTRY ?= registry.example.com/atsap
IMAGE_TAG ?= latest
# --env-file: compose interpolates ${...} from the PROJECT directory
# (deploy/), not the repo root, so the root .env is passed explicitly — it
# carries ATSAPBX_DEV_LICENCE_PUBKEY into the app's build args.
COMPOSE := docker compose $(if $(wildcard .env),--env-file .env) -f deploy/docker-compose.yml
COMPOSE_PROD := docker compose -f deploy/docker-compose.prod.yml

.DEFAULT_GOAL := help

##@ General

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_.-]+:.*?##/ { printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Local stack (compose)

.PHONY: env dev dev-licence dev-down dev-app dev-logs rig-restore
env: ## Create .env from .env.example if missing
	test -f .env || cp .env.example .env

dev: env ## Start dev stack (Asterisk + Postgres + NATS + app)
	$(COMPOSE) up --build

# The dev app boots in Setup — no call path — because a real build trusts
# only the vendor key (D-52, D-54). This creates a throwaway keypair under
# the gitignored .dev-licence/, writes its public key and a token it
# signed into .env, and rebuilds the app to trust that key. Nothing is
# committed, and images-push builds with the key explicitly empty
# (pbx-call-placement design D8).
dev-licence: env ## Licence the local dev app with a throwaway dev key
	mkdir -p .dev-licence
	cd api && go run ./cmd/dev-licence -dir ../.dev-licence > ../.dev-licence/env.lines.tmp
	grep -v '^ATSAPBX_DEV_LICENCE_PUBKEY=\|^ATSAPBX_LICENSE_TOKEN=' .env > .dev-licence/env.tmp || true
	cat .dev-licence/env.lines.tmp >> .dev-licence/env.tmp
	mv .dev-licence/env.tmp .env && rm -f .dev-licence/env.lines.tmp
	docker compose --env-file .env -f deploy/docker-compose.yml up -d --build app

dev-down: ## Stop and remove the dev stack
	$(COMPOSE) down

dev-app: ## Rebuild + restart only the app service (fast; Asterisk untouched)
	$(COMPOSE) up -d --build app

dev-logs: ## Tail the dev stack's logs
	$(COMPOSE) logs -f

# Restore the dev rig between test tiers. Two independent things rot, and
# neither symptom names its cause:
#
#   1. The integration tier's cleanup drops the schema (each suite
#      migrates down on exit), leaving e2e facing a database with no
#      tables and sip-ua deregistered — since ps_contacts became
#      realtime-backed, registrations live in Postgres (D-47).
#
#   2. (FIXED in Asterisk 22.11.0 — kept as history, and as the reason
#      the engine version is pinned.) Asterisk's ARI WebSocket used to
#      stop accepting event streams after serving a handful of them: the
#      HTTP API still answered 200, but every stream dial timed out and
#      the app logged "ari: event stream disconnected, retrying" on a
#      growing backoff. The test-side symptom was "timed out waiting for
#      Stasis app voip-app-e2e to register", which names neither Asterisk
#      nor WebSockets and sends you looking at your own test. Root cause
#      was upstream issue #2020, an inbound websocket session leak;
#      restarting Asterisk was the only workaround. If this ever
#      returns, check the engine version FIRST
#      (docs/ASTERISK-INTEGRATION.md §2.1.1) before suspecting the test.
#      The identical symptom on 22.11.0 is a different cause — an ARI
#      teardown deadlock, upstream #2170 — avoided by never requesting
#      subscribeAll (§2.1.2). Restarting Asterisk clears either.
#
# Order matters: Asterisk first (it owns the ARI listener and the
# registrations), then the app (it migrates on boot), then sip-ua (it
# must register against the schema that results).
rig-restore: ## Restore the dev rig between test tiers
	docker restart deploy-asterisk-1 >/dev/null
	sleep 18
	docker restart deploy-app-1 >/dev/null
	sleep 8
	docker restart deploy-sipua-1 >/dev/null
	sleep 12
	docker exec deploy-asterisk-1 asterisk -rx 'pjsip show contacts'

##@ Local stack (Tilt)

.PHONY: tilt-up tilt-down tilt-ci
tilt-up: env ## Start stack under Tilt (live reload for Go services; UI :10350)
	tilt up

tilt-down: ## Stop the Tilt-managed stack
	tilt down

tilt-ci: env ## Headless Tilt bring-up; exits 0 once all workloads are healthy
	tilt ci

##@ Go

.PHONY: build run test lint vuln proto tidy
build: ## Build the Go binary to ./bin/atsap-api
	cd api && CGO_ENABLED=0 go build -o ../bin/atsap-api ./cmd/atsap-api

run: ## Run the Go app locally (expects Asterisk reachable, see .env)
	cd api && go run ./cmd/atsap-api

test: ## Run the Go test suite (race + cover, in api/)
	cd api && go test ./... -race -cover

# --build-tags is not optional. Without it golangci-lint never compiles a
# file behind //go:build integration or //go:build e2e, so depguard's
# boundary rules — the architectural gate — silently skipped every
# integration and e2e test in the repo. That went unnoticed because the
# untagged run reports "0 issues", which reads like coverage rather than
# absence. Adding the tags surfaced nine real findings on 2026-09-08.
lint: ## Run golangci-lint (including tagged integration and e2e files)
	cd api && golangci-lint run --build-tags integration,e2e ./...

vuln: ## Run govulncheck (pinned, matches CI GOVULNCHECK_VERSION)
	cd api && go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

# buf.yaml has configured lint and breaking rules since LLD-01, but
# nothing ever ran them — the proto contract was unguarded while
# docs/API.md claimed otherwise. A reshape of an RPC that already has a
# consumer now fails here rather than in a partner's client.
#
# Compared against origin/develop, the integration branch, rather than
# main: main still predates the protos entirely (it has no .proto files
# at all), so it cannot serve as a baseline yet. Move this to main once
# develop is promoted and main carries the contract.
proto: ## Lint protos + breaking-change check against the integration branch
	cd api && buf lint && buf breaking --against '../.git#branch=origin/develop,subdir=api'

tidy: ## Tidy go modules
	cd api && go mod tidy

##@ Gates and docs

.PHONY: docs diagrams ci
docs: ## Doc wiring checks (HLD markers, decision refs, internal links)
	./scripts/check-docs.sh

# Not in `ci`: unlike check-docs.sh this needs mermaid-cli and a Chromium
# binary, which the CI image does not carry yet. Run it whenever a
# diagram changes; wiring it into Jenkins means adding chromium there.
diagrams: ## Render every mermaid diagram in docs/ (needs Chromium; not in ci)
	./scripts/check-diagrams.sh

ci: docs lint vuln test ## Full local gate in pipeline order

##@ Images, UAT and deploy

.PHONY: docker-build images-push uat-auto uat-placement uat-inbound deploy
docker-build: ## Build production images for the app and Asterisk
	docker build -t atsap-api:local api && docker build -t atsap-core:local core

images-push: ## Build, tag and push images to REGISTRY (IMAGE_TAG; defaults above)
	set -eu; \
	docker build --build-arg DEV_LICENCE_PUBKEY= -t "$(REGISTRY)/atsap-api:$(IMAGE_TAG)" api; \
	docker build -t "$(REGISTRY)/atsap-core:$(IMAGE_TAG)" core; \
	docker push "$(REGISTRY)/atsap-api:$(IMAGE_TAG)"; \
	docker push "$(REGISTRY)/atsap-core:$(IMAGE_TAG)"

uat-auto: ## Automated UAT on real baresip phones (rig restored after)
	./scripts/uat-auto.sh

uat-placement: ## UAT: PlaceCall over a carrier trunk between real baresip phones (needs make dev-licence)
	./scripts/uat-placement.sh

uat-inbound: ## UAT: carrier→DID→phone and desk phone→trunk between real baresip phones (needs make dev-licence)
	./scripts/uat-inbound.sh

deploy: ## Deploy prod stack on this host from pre-built images (VPS; see README)
	$(COMPOSE_PROD) pull && $(COMPOSE_PROD) up -d
