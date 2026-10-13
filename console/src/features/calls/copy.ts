// Words for call values. The value itself is never replaced by a
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

// ENGINE_VOCABULARY is the media engine's identifiers and words. Server
// text is domain language ("normal clearing"); should any of these ever
// appear in it, the console withholds the text rather than show the
// engine (console brief §6 rule 10).
const ENGINE_VOCABULARY = [
  /\b[et]_[0-9a-f]{32}\b/,
  /\b(?:PJSIP|SIP|IAX2|Local|DAHDI)\//,
  /\b1[6-9]\d{8}\.\d+\b/,
  /\bbridge[-_][0-9a-f-]{8,}/i,
  /\bps_[a-z_]+\b/,
  /\b[\w-]+\.conf\b/,
  /\bstasis-(?:in|out)\b/,
  /\basterisk\b/i,
];

export const WITHHELD = "Not shown";

// serverText returns server-written text as it came, or WITHHELD when it
// carries engine vocabulary.
export function serverText(text: string): string {
  return ENGINE_VOCABULARY.some((p) => p.test(text)) ? WITHHELD : text;
}
