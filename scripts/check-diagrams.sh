#!/usr/bin/env bash
# Render every ```mermaid block in docs/ and fail if any of them does not
# parse. A diagram that silently stopped rendering is exactly the kind of
# documentation rot D-28 says to catch with a machine, not a reviewer.
#
# Deliberately NOT part of scripts/check-docs.sh: that script runs
# anywhere with coreutils and no network. This one needs mermaid-cli and a
# Chromium binary, so it is its own task (`mise run diagrams`) and says so
# loudly when the tools are missing rather than passing vacuously.

set -u

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT" || exit 2

if ! command -v mmdc > /dev/null 2>&1; then
    echo "FAIL: mmdc (mermaid-cli) not found. Install it, or run 'mise install'."
    exit 2
fi

CHROME=""
for candidate in "${PUPPETEER_EXECUTABLE_PATH:-}" /usr/bin/chromium \
                 /usr/bin/chromium-browser /usr/bin/google-chrome-stable; do
    if [ -n "$candidate" ] && [ -x "$candidate" ]; then
        CHROME="$candidate"
        break
    fi
done

if [ -z "$CHROME" ]; then
    echo "FAIL: no Chromium/Chrome binary found; mermaid-cli cannot render."
    echo "      Set PUPPETEER_EXECUTABLE_PATH to one."
    exit 2
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

cat > "$WORK/puppeteer.json" <<EOF
{ "executablePath": "$CHROME", "args": ["--no-sandbox", "--disable-gpu"] }
EOF

# Split each markdown file into numbered .mmd fragments, remembering which
# file and which block each came from so a failure names them.
python3 - "$WORK" <<'PY'
import pathlib, re, sys

work = pathlib.Path(sys.argv[1])
index = []
for md in sorted(pathlib.Path("docs").rglob("*.md")):
    blocks = re.findall(r"```mermaid\n(.*?)```", md.read_text(), re.S)
    for n, block in enumerate(blocks, start=1):
        name = f"{len(index):03d}.mmd"
        (work / name).write_text(block)
        index.append(f"{name}\t{md}\tblock {n}")
(work / "index.tsv").write_text("\n".join(index) + "\n" if index else "")
print(f"found {len(index)} mermaid diagrams")
PY

failures=0
while IFS=$'\t' read -r frag source block; do
    [ -z "${frag:-}" ] && continue
    if ! mmdc -p "$WORK/puppeteer.json" -i "$WORK/$frag" \
              -o "$WORK/$frag.svg" > "$WORK/$frag.log" 2>&1; then
        echo "FAIL: $source ($block) does not render"
        grep -iE 'error|expecting|parse' "$WORK/$frag.log" | head -3
        failures=$((failures + 1))
    fi
done < "$WORK/index.tsv"

if [ "$failures" -ne 0 ]; then
    echo "== $failures mermaid diagram(s) failed to render =="
    exit 1
fi

echo "== all mermaid diagrams render =="
