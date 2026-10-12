#!/usr/bin/env bash
# coverage-go.sh — Go coverage across the test tiers, merged
# (quality-and-supply-chain-gates, D-71).
#
#   scripts/coverage-go.sh            # unit only (hermetic)
#   TIERS="unit integration e2e" scripts/coverage-go.sh   # every tier (needs the Tilt rig)
#
# Each tier writes Go's binary coverage data into coverage/go/<tier>:
#   - unit and integration: `go test -cover -args -test.gocoverdir`;
#   - e2e: the e2e tests' own counters plus the running app's. Tilt's dev
#     image is built with -cover and GOCOVERDIR=/cover, and writes its
#     counters when it exits, so the app is restarted before (to start
#     clean) and after (to flush) the e2e run, and /cover is copied out.
# The tiers are merged with `go tool covdata`, written as a text profile
# (coverage/go/merged.out) and as Cobertura (coverage/go/cobertura.xml),
# and checked against quality/coverage-floors.json by cmd/covergate.
#
# The integration tier resets the dev database's schema (docs/TESTING.md
# §5), so the script runs `make rig-restore` and `make dev-user` after it.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/coverage/go"
TIERS="${TIERS:-unit}"
COBERTURA="github.com/boumenot/gocover-cobertura@v1.5.0"

cd "$ROOT/api"
# Our code only: generated protobuf and the dev-only tools are excluded
# from the denominator.
PKGS="$(go list ./... | grep -v -e /genproto -e /cmd/ari-playground -e /cmd/sip-ua -e /cmd/dev-licence -e /cmd/covergate | paste -sd, -)"

rm -rf "$OUT"
mkdir -p "$OUT/merged"
inputs=()

for tier in $TIERS; do
	dir="$OUT/$tier"
	mkdir -p "$dir"
	case "$tier" in
	unit)
		# gotestsum (pinned in .mise.toml) also writes JUnit for CI.
		gotestsum --junitfile "$OUT/unit-junit.xml" -- -race -cover -covermode=atomic -coverpkg="$PKGS" ./... -args -test.gocoverdir="$dir"
		;;
	integration)
		go test -tags integration -p 1 -race -cover -covermode=atomic -coverpkg="$PKGS" ./... -args -test.gocoverdir="$dir"
		(cd "$ROOT" && make rig-restore >/dev/null && make dev-user >/dev/null)
		;;
	e2e)
		docker exec deploy-app-1 sh -c 'test -n "$GOCOVERDIR"' ||
			{ echo "coverage-go: deploy-app-1 is not the instrumented dev image (run under Tilt)" >&2; exit 1; }
		docker restart deploy-app-1 >/dev/null
		docker exec deploy-app-1 sh -c 'rm -f /cover/*'
		until curl -sf http://localhost:8080/healthz >/dev/null; do sleep 1; done
		go test -tags e2e -p 1 -count=1 -cover -covermode=atomic -coverpkg="$PKGS" ./... -args -test.gocoverdir="$dir"
		docker restart deploy-app-1 >/dev/null
		docker cp deploy-app-1:/cover/. "$dir/"
		until curl -sf http://localhost:8080/healthz >/dev/null; do sleep 1; done
		;;
	*)
		echo "coverage-go: unknown tier $tier" >&2
		exit 2
		;;
	esac
	inputs+=("$dir")
done

go tool covdata merge -i="$(IFS=,; echo "${inputs[*]}")" -o "$OUT/merged"
go tool covdata textfmt -i="$OUT/merged" -pkg="$PKGS" -o "$OUT/merged.out"
go run "$COBERTURA" <"$OUT/merged.out" >"$OUT/cobertura.xml"

for tier in "${inputs[@]}"; do
	printf '%-12s ' "$(basename "$tier")"
	go tool covdata percent -i="$tier" -pkg="$PKGS" | awk '{gsub("%","",$3); n++; s+=$3} END {printf "%d packages\n", n}'
done
go tool cover -func="$OUT/merged.out" | tail -1
# Gate against the floors for the tiers measured: "unit" alone, or "all".
case "$TIERS" in
"unit") section=unit ;;
"unit integration e2e") section=all ;;
*)
	echo "coverage-go: no floors for TIERS=\"$TIERS\"; measured, not gated"
	exit 0
	;;
esac
go run ./cmd/covergate -profile "$OUT/merged.out" -floors "$ROOT/quality/coverage-floors.json" -section "$section" ${RATCHET:+-ratchet}
