import { TrunkHealth, TrunkHealthReason } from "../../api/gen/atsapbx/v1/pbx_pb";
import type { Tone } from "../../components/icons";

// Observed trunk health, in words. Health is what the platform has
// observed — its own checks of the carrier and calls through the trunk —
// never a setting (CONSOLE-DESIGN "Trunks").
export const trunkHealth: Record<TrunkHealth, { label: string; tone: Tone }> = {
  [TrunkHealth.UNSPECIFIED]: { label: "Not yet observed", tone: "neutral" },
  [TrunkHealth.UNKNOWN]: { label: "Not checked yet", tone: "neutral" },
  [TrunkHealth.HEALTHY]: { label: "Healthy", tone: "success" },
  [TrunkHealth.DEGRADED]: { label: "Degraded", tone: "warning" },
  [TrunkHealth.UNHEALTHY]: { label: "Unhealthy", tone: "danger" },
};

// Why a trunk has its health, in words (pbx-trunk-health-probing). Each
// is what the platform's check of the carrier, or a call through it,
// found — never a guess.
export const healthReason: Record<TrunkHealthReason, string | null> = {
  [TrunkHealthReason.UNSPECIFIED]: null,
  [TrunkHealthReason.ANSWERING]: "Answering",
  [TrunkHealthReason.SLOW_RESPONSE]: "Slow to respond",
  [TrunkHealthReason.HOST_NOT_FOUND]: "Host not found",
  [TrunkHealthReason.NO_RESPONSE]: "No response",
  [TrunkHealthReason.CARRIER_ERROR]: "Carrier error",
};

const timeOnly = new Intl.DateTimeFormat(undefined, { timeStyle: "medium" });
const dateAndTime = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "medium" });

// checkedAt says when a trunk was last observed: the time today, the date
// and time otherwise.
export function checkedAt(at: Date, now: Date = new Date()): string {
  return at.toDateString() === now.toDateString() ? timeOnly.format(at) : dateAndTime.format(at);
}
