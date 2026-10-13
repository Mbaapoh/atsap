// Prefix rules the console mirrors from pbx-core (pbx/domain/route.go): a
// pattern is digits and X (any one digit), with an optional leading "+",
// which is ignored when matching.

export const PREFIX = /^\+?[0-9X]{1,50}$/; // MaxPrefixPatternLength = 50

function body(p: string): string {
  return p.startsWith("+") ? p.slice(1) : p;
}

// overlaps reports whether some dialled number could match both patterns:
// over the shorter length, every position agrees or one side is X.
export function overlaps(a: string, b: string): boolean {
  const x = body(a);
  const y = body(b);
  const n = Math.min(x.length, y.length);
  for (let i = 0; i < n; i++) {
    if (x[i] !== y[i] && x[i] !== "X" && y[i] !== "X") return false;
  }
  return n > 0;
}

// Cost is stored in micro-units per minute and only ever displayed
// (LLD-03 §9): the platform never computes a price from it.
export function parseCost(text: string): bigint | null {
  const t = text.trim();
  if (t === "") return 0n;
  const m = /^(\d{1,12})(?:\.(\d{1,6}))?$/.exec(t);
  if (!m) return null;
  return BigInt(m[1]!) * 1_000_000n + BigInt((m[2] ?? "").padEnd(6, "0"));
}

export function formatCost(micros: bigint): string {
  const whole = micros / 1_000_000n;
  const frac = (micros % 1_000_000n).toString().padStart(6, "0").replace(/0+$/, "");
  return frac ? `${whole}.${frac}` : `${whole}`;
}
