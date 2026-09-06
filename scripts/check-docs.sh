#!/usr/bin/env bash
# Validate the AtsaPBX documentation wiring that CI must not let silently
# rot (HLD docs/README.md §5.1 "Verification & Acceptance Criteria"; TOOLSET
# D-28 machine gates):
#
#   1. All seventeen <!-- OpenSpec: TRD-HLD-NN --> markers (01-17) exist.
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

echo "== 1. HLD markers (TRD-HLD-01 .. TRD-HLD-17) =="
for n in $(seq -w 1 17); do
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

if [ "$failures" -ne 0 ]; then
    echo "== $failures documentation check(s) failed =="
    exit 1
fi

echo "== docs checks passed =="
