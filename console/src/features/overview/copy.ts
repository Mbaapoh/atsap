export { trunkHealth } from "../telephony/copy";

// Words for server values. The value itself is never replaced by a
// different meaning; an unknown value is shown as the server sent it.

const directions: Record<string, string> = { INBOUND: "Inbound", OUTBOUND: "Outbound", INTERNAL: "Internal" };

export function directionLabel(direction: string): string {
  return directions[direction] ?? direction;
}


// durationLabel writes a call's answered duration; a call never
// answered has none rather than "0:00".
export function durationLabel(seconds: number): string {
  if (seconds <= 0) return "—";
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}:${String(s).padStart(2, "0")}`;
}
