// The console's adapter boundary (HLD-19 §3, console-foundation task 1.3),
// as rules over the source tree. boundary.test.ts runs them against every
// file under src/, and against deliberately broken files to prove each
// rule fires.

export interface SourceFile {
  // path relative to the console root, e.g. "src/api/transport.ts".
  path: string;
  text: string;
}

export interface Violation {
  path: string;
  rule: string;
}

const isTest = (p: string) => /\.test\.tsx?$/.test(p) || p.startsWith("src/test/");
const inApi = (p: string) => p.startsWith("src/api/");
const isAdapter = (p: string) => /^src\/features\/[^/]+\/adapter\.ts$/.test(p);
const isCompositionRoot = (p: string) => p === "src/app/App.tsx";

const importPattern = /(?:import|export)\s[^'"]*?from\s*["']([^"']+)["']|import\s*\(\s*["']([^"']+)["']\s*\)|import\s+["']([^"']+)["']/g;

// imports lists the modules a file imports, with relative specifiers
// resolved to console-root paths without extension ("src/api/clients").
export function imports(file: SourceFile): string[] {
  const dir = file.path.split("/").slice(0, -1);
  const out: string[] = [];
  for (const m of file.text.matchAll(importPattern)) {
    const spec = m[1] ?? m[2] ?? m[3];
    if (!spec) continue;
    if (!spec.startsWith(".")) {
      out.push(spec);
      continue;
    }
    const parts = [...dir];
    for (const seg of spec.split("/")) {
      if (seg === "..") parts.pop();
      else if (seg !== ".") parts.push(seg);
    }
    out.push(parts.join("/").replace(/\.(tsx?|css)$/, ""));
  }
  return out;
}

interface Rule {
  name: string;
  check: (file: SourceFile, deps: string[]) => boolean; // true = violation
}

const rules: Rule[] = [
  {
    // The transport library is an API-layer detail.
    name: "only src/api may import @connectrpc",
    check: (f, deps) => !inApi(f.path) && !f.path.startsWith("src/test/") && deps.some((d) => d.startsWith("@connectrpc/")),
  },
  {
    // Protobuf runtime helpers belong to the API layer and the adapters
    // that map wire messages to view models.
    name: "only src/api and feature adapters may import @bufbuild",
    check: (f, deps) => !inApi(f.path) && !isAdapter(f.path) && deps.some((d) => d.startsWith("@bufbuild/")),
  },
  {
    // Transport and clients are built once, at the composition root.
    name: "only src/api and the composition root may import the transport or clients",
    check: (f, deps) =>
      !inApi(f.path) && !isCompositionRoot(f.path) && !isTest(f.path) && deps.some((d) => d === "src/api/transport" || d === "src/api/clients"),
  },
  {
    // A generated client is reached through an adapter, never a component.
    name: "only feature adapters may use the API clients",
    check: (f) => !inApi(f.path) && !isAdapter(f.path) && /\buseApiClients\b/.test(f.text),
  },
  {
    // Every request goes through the transport and its public-only guard.
    name: "only the transport may open a network request",
    check: (f) =>
      f.path !== "src/api/transport.ts" &&
      !isTest(f.path) &&
      /\bfetch\s*\(|\bXMLHttpRequest\b|\bnew\s+WebSocket\b|\bEventSource\b|\bsendBeacon\b|\baxios\b/.test(f.text),
  },
  {
    // Procedure paths come from generated descriptors, not string literals.
    name: "no hand-written procedure paths outside src/api",
    check: (f) => !inApi(f.path) && !isTest(f.path) && /["'`]\/?atsapbx\.v1\./.test(f.text),
  },
  {
    // Shared components are domain-neutral: no API, no feature.
    name: "src/components may not import the API or a feature",
    check: (f, deps) => f.path.startsWith("src/components/") && deps.some((d) => d.startsWith("src/api/") || d.startsWith("src/features/")),
  },
];

// RULE_NAMES lets the tests prove each rule by name.
export const RULE_NAMES = rules.map((r) => r.name);

export function violations(files: SourceFile[]): Violation[] {
  const out: Violation[] = [];
  for (const f of files) {
    if (f.path.startsWith("src/api/gen/")) continue;
    const deps = imports(f);
    for (const r of rules) {
      if (r.check(f, deps)) out.push({ path: f.path, rule: r.name });
    }
  }
  return out;
}
