#!/usr/bin/env bash
# Validate the AtsaPBX documentation wiring that CI must not let silently
# rot (HLD docs/README.md §5.1 "Verification & Acceptance Criteria"; TOOLSET
# D-28 machine gates):
#
#   1. All nineteen <!-- OpenSpec: TRD-HLD-NN --> markers (01-19) exist.
#   2. Every "D-NN" decision citation in docs/, openspec/ and README.md
#      resolves to an entry in docs/DECISIONS.md (no orphan citations such
#      as the former D-38/D-39 gap).
#   3. Every internal relative .md link in docs/ resolves to a real file.
#
# Exit non-zero on any failure so CI fails loudly. No third-party tools;
# runs with grep/sed/awk/coreutils.

set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT" || exit 2

failures=0

note_fail() {
    echo "FAIL: $1"
    failures=$((failures + 1))
}

echo "== 1. HLD markers (TRD-HLD-01 .. TRD-HLD-19) =="
for n in $(seq -w 1 19); do
    if ! grep -rq --include='*.md' "OpenSpec: TRD-HLD-$n" docs/hld; then
        note_fail "missing marker <!-- OpenSpec: TRD-HLD-$n --> in docs/hld/"
    fi
done

echo "== 2. Decision citations resolve to docs/DECISIONS.md =="
declog="docs/DECISIONS.md"
have="$(grep -oE '^\*\*D-[0-9]{2}' "$declog" | grep -oE '[0-9]{2}' | sort -u)"
cite_files=$(find docs openspec README.md -type f -name '*.md' -not -path 'docs/DECISIONS.md' 2>/dev/null)
cites="$(printf '%s\n' "$cite_files" | xargs grep -ohE '\bD-[0-9]{2}\b' 2>/dev/null | grep -oE '[0-9]{2}' | sort -u)"
for c in $cites; do
    if ! printf '%s\n' "$have" | grep -qx "$c"; then
        note_fail "D-$c is cited but has no entry in $declog"
    fi
done

echo "== 3. Internal relative .md links resolve =="
# Walk docs/*.md; for each line NOT inside a ``` code fence, find ](target)
# markdown links; skip absolute URLs/anchors; resolve relative to the file.
while IFS= read -r f; do
    in_fence=0
    while IFS= read -r line; do
        if printf '%s\n' "$line" | grep -qE '^```'; then
            if [ "$in_fence" -eq 1 ]; then in_fence=0; else in_fence=1; fi
            continue
        fi
        [ "$in_fence" -eq 1 ] && continue
        printf '%s\n' "$line" | grep -oE '\]\([^)]+\)' | sed -E 's/^\]\((.*)\)$/\1/' | while IFS= read -r target; do
            case "$target" in
                http://*|https://*|mailto:*|'#'*|'') continue ;;
            esac
            # strip optional anchor
            path="${target%%#*}"
            [ -z "$path" ] && continue
            dir="$(dirname "$f")"
            if [ ! -e "$dir/$path" ]; then
                echo "FAIL: broken link in $f -> $target"
                # propagate failure out of the subshell
                echo "BROKEN:$f->$target" >> /tmp/atsap-doclinks.$$
            fi
        done
    done < "$f"
done < <(find docs -type f -name '*.md' | sort)

if [ -f /tmp/atsap-doclinks.$$ ]; then
    failures=$((failures + $(wc -l < /tmp/atsap-doclinks.$$)))
    rm -f /tmp/atsap-doclinks.$$
fi

# 4. Every RPC defined in a .proto appears in docs/API.md §1.
#
# The API surface doc is only useful if it cannot drift from the protos
# it describes (D-43: the change that changes the surface updates the
# doc). Machine gate rather than review discipline, per D-28.
echo "== 4. Every proto RPC is listed in docs/API.md =="
apidoc="docs/API.md"
if [ ! -f "$apidoc" ]; then
    note_fail "docs/API.md is missing — it is the living API surface inventory"
elif [ -d api/proto ]; then
    while read -r rpc; do
        [ -z "$rpc" ] && continue
        if ! grep -q "\`$rpc\`" "$apidoc"; then
            note_fail "rpc $rpc is defined in api/proto but absent from docs/API.md (D-43)"
        fi
    done < <(grep -rhoE '^[[:space:]]*rpc[[:space:]]+[A-Za-z0-9_]+' api/proto --include='*.proto' \
                | awk '{print $2}' | sort -u)
fi

# 5. Every bounded context in HLD 04 §10.1 has an LLD row and a section.
#
# Ten documents enumerate the bounded contexts (the HLD graph and matrix,
# the TRD quick view, the architecture package tree, the LLD index, the
# D-48 map). Adding a context means touching all of them, and on
# 2026-09-09 adding `entitlement` proved the obvious: without a gate, the
# ones nobody is looking at go stale — hld/README.md still claimed "9
# bounded contexts" long after it was wrong.
#
# This checks the two that matter for build order — one LLD per context
# (D-48) and a section describing it — because those are the ones an
# agent reads before proposing a change. Prose counts elsewhere are not
# checkable and are not claimed here (D-28).
echo "== 5. Every bounded context in HLD 04 §10.1 has an LLD =="
ctxdoc="docs/hld/04-bounded-contexts.md"
lldindex="docs/lld/README.md"
if [ -f "$ctxdoc" ] && [ -f "$lldindex" ]; then
    # The matrix rows look like: | `context` | may depend on | must not |
    contexts="$(sed -n '/### 10.1 Allowed-dependency matrix/,/^A pull request/p' "$ctxdoc" \
        | grep -oE '^\| `[a-z-]+`' | grep -oE '[a-z-]+' | grep -v '^$' | sort -u)"
    if [ -z "$contexts" ]; then
        note_fail "could not parse any context from $ctxdoc §10.1 — the matrix format changed, and check 5 is now blind"
    fi
    # Scope the search to the index TABLE, not the whole file: a context
    # is mentioned in the delivery-phase prose too, and matching that
    # would let a deleted index row pass. Found by fault injection —
    # the first version of this check did exactly that.
    indextable="$(sed -n '/^## Index/,/^The console is deliberately absent/p' "$lldindex")"
    for ctx in $contexts; do
        if ! printf '%s\n' "$indextable" | grep -q "| \`$ctx\` |"; then
            note_fail "bounded context '$ctx' is in $ctxdoc §10.1 but has no row in $lldindex's Index table (D-48: one LLD per context)"
        fi
        if ! grep -qE "^## [0-9a-z]+\. \`$ctx\`" "$ctxdoc"; then
            note_fail "bounded context '$ctx' is in the §10.1 matrix but has no '## N. \`$ctx\`' section in $ctxdoc"
        fi
    done
fi

if [ "$failures" -ne 0 ]; then
    echo "== $failures documentation check(s) failed =="
    exit 1
fi

echo "== docs checks passed =="
