import { errorDetail, toApiError } from "../../api/errors";

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

// DIALLED is the server's destination rule (pbx/domain
// ValidateDestination): an optional leading + then 1-32 digits.
export const DIALLED = /^\+?[0-9]{1,32}$/;

// PlaceCallFailure is a placement refusal in console words. It carries
// fixed copy, never the server's message, so nothing raw can reach a
// dialog.
export class PlaceCallFailure extends Error {
  constructor(message: string) {
    super(message);
    this.name = "PlaceCallFailure";
  }
}

// placeCallError maps the server's fixed placement preconditions
// (pbx/rpc mapCallErr) to console words by matching those exact prefixes,
// exactly as the server names them. It never shows the raw message: an
// unrecognised one falls back to the generic fixed copy.
export function placeCallError(err: unknown): Error {
  const detail = errorDetail(err);
  if (detail.kind === "conflict") {
    const raw = detail.rawMessage;
    if (raw.startsWith("destination is unroutable")) {
      return new PlaceCallFailure("No extension or outbound route matches that number. Check it and try again.");
    }
    if (raw.startsWith("trunk capacity")) {
      return new PlaceCallFailure("Every trunk for that number is at its channel limit. Try again shortly.");
    }
    if (raw.startsWith("the calling extension has no registered device")) {
      return new PlaceCallFailure("That extension has no registered device. Register a phone to it first.");
    }
    if (raw.startsWith("call refused")) {
      return new PlaceCallFailure("The platform refused this call. Check the number and the extension, then try again.");
    }
  }
  return toApiError(err);
}

// serverText returns server-written text as it came, or WITHHELD when it
// carries engine vocabulary.
export function serverText(text: string): string {
  return ENGINE_VOCABULARY.some((p) => p.test(text)) ? WITHHELD : text;
}
