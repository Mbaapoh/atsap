// The call-history filters live in the address (console-admin-screens
// D7), so Back, reload and a shared link keep them. Dates are whole local
// days; "to" is inclusive on screen and sent as the next midnight,
// because ListCalls takes [started_from, started_to).

// CALL_STATES are the PRD §11.1 lifecycle names, exactly as the server
// uses them (telephony/domain CallState). No Answered, Missed or Failed.
export const CALL_STATES = ["Initiated", "Screening", "Routing", "Presenting", "Active", "Degraded", "Terminating", "Terminated"] as const;
export const DIRECTIONS = ["INBOUND", "OUTBOUND", "INTERNAL"] as const;

export interface CallFilters {
  from: string; // YYYY-MM-DD or ""
  to: string;
  direction: string;
  state: string;
  page: number; // zero-based
}

const DAY = /^\d{4}-\d{2}-\d{2}$/;

// validDay keeps a real calendar day only: Date would roll "2026-13-45"
// over into a later month, so the parts must come back unchanged.
function validDay(v: string | null): string {
  if (!v || !DAY.test(v)) return "";
  const [y = 0, m = 0, d = 0] = v.split("-").map(Number);
  const date = localMidnight(v);
  return date.getFullYear() === y && date.getMonth() === m - 1 && date.getDate() === d ? v : "";
}

// readFilters reads the address, dropping anything it does not
// recognise rather than sending it on.
export function readFilters(params: URLSearchParams): CallFilters {
  const direction = params.get("direction") ?? "";
  const state = params.get("state") ?? "";
  const page = Number(params.get("page") ?? "1");
  return {
    from: validDay(params.get("from")),
    to: validDay(params.get("to")),
    direction: (DIRECTIONS as readonly string[]).includes(direction) ? direction : "",
    state: (CALL_STATES as readonly string[]).includes(state) ? state : "",
    page: Number.isInteger(page) && page >= 1 ? page - 1 : 0,
  };
}

export function writeFilters(f: CallFilters): URLSearchParams {
  const out = new URLSearchParams();
  if (f.from) out.set("from", f.from);
  if (f.to) out.set("to", f.to);
  if (f.direction) out.set("direction", f.direction);
  if (f.state) out.set("state", f.state);
  if (f.page > 0) out.set("page", String(f.page + 1));
  return out;
}

export function localMidnight(day: string): Date {
  const [y = 0, m = 1, d = 1] = day.split("-").map(Number);
  return new Date(y, m - 1, d);
}

// range turns the on-screen days into the half-open instant range sent.
export function range(f: CallFilters): { from?: Date; to?: Date } {
  const out: { from?: Date; to?: Date } = {};
  if (f.from) out.from = localMidnight(f.from);
  if (f.to) {
    const end = localMidnight(f.to);
    end.setDate(end.getDate() + 1);
    out.to = end;
  }
  return out;
}
