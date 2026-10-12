import { describe, expect, it } from "vitest";
import { imports, RULE_NAMES, violations, type SourceFile } from "./boundary";

// Every source file under src/, read as text at test time.
const raw = import.meta.glob<string>(["../**/*.{ts,tsx}", "!../api/gen/**", "!./boundary*.ts"], {
  query: "?raw",
  import: "default",
  eager: true,
});
// Glob keys are relative to this directory: "../api/x.ts" or "./msw.ts".
const toPath = (key: string) => (key.startsWith("./") ? `src/test/${key.slice(2)}` : `src/${key.replace(/^\.\.\//, "")}`);
const tree: SourceFile[] = Object.entries(raw).map(([key, text]) => ({ path: toPath(key), text }));

describe("adapter boundary", () => {
  it("reads the real source tree", () => {
    const paths = tree.map((f) => f.path);
    expect(paths).toContain("src/api/transport.ts");
    expect(paths).toContain("src/features/licence/adapter.ts");
    expect(paths).toContain("src/components/Modal.tsx");
    expect(paths).toContain("src/test/msw.ts");
  });

  it("holds across the console", () => {
    expect(violations(tree)).toEqual([]);
  });

  it("resolves relative imports to console paths", () => {
    expect(imports({ path: "src/features/x/Page.tsx", text: 'import { a } from "../../api/clients";\nimport "./x.css";' })).toEqual([
      "src/api/clients",
      "src/features/x/x",
    ]);
  });

  // One deliberately broken file per rule: each must be caught, by that
  // rule, so a rule that silently stops matching fails here.
  const broken: { rule: string; file: SourceFile }[] = [
    {
      rule: "only src/api may import @connectrpc",
      file: { path: "src/features/x/Page.tsx", text: 'import { createClient } from "@connectrpc/connect";' },
    },
    {
      rule: "only src/api and feature adapters may import @bufbuild",
      file: { path: "src/features/x/Page.tsx", text: 'import { create } from "@bufbuild/protobuf";' },
    },
    {
      rule: "only src/api and the composition root may import the transport or clients",
      file: { path: "src/app/AppShell.tsx", text: 'import { createApiTransport } from "../api/transport";' },
    },
    {
      rule: "only feature adapters may use the API clients",
      file: { path: "src/features/x/Page.tsx", text: "const { pbx } = useApiClients();" },
    },
    {
      rule: "only the transport may open a network request",
      file: { path: "src/features/x/adapter.ts", text: 'await fetch("/readyz");' },
    },
    {
      rule: "no hand-written procedure paths outside src/api",
      file: { path: "src/features/x/adapter.ts", text: 'const path = "/atsapbx.v1.PbxService/ListExtensions";' },
    },
    {
      rule: "src/components may not import the API or a feature",
      file: { path: "src/components/Widget.tsx", text: 'import { useLicenceStatus } from "../features/licence/adapter";' },
    },
  ];

  it("has a broken case for every rule", () => {
    expect(broken.map((b) => b.rule).sort()).toEqual([...RULE_NAMES].sort());
  });

  it.each(broken)("catches: $rule", ({ rule, file }) => {
    expect(violations([file]).map((v) => v.rule)).toContain(rule);
  });

  it("lets an adapter use the clients and protobuf helpers", () => {
    const adapter: SourceFile = {
      path: "src/features/x/adapter.ts",
      text: 'import { timestampDate } from "@bufbuild/protobuf/wkt";\nimport { useApiClients } from "../../api/ApiProvider";\nconst { pbx } = useApiClients();',
    };
    expect(violations([adapter])).toEqual([]);
  });
});
