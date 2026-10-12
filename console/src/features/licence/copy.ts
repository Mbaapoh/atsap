import { CapacityLevel, LicenseCondition, LicenseState, LicenseTier } from "../../api/gen/atsapbx/v1/licensing_pb";
import type { LicenceSummary } from "./adapter";

// Console wording for the licence state. Every word here describes a
// value the server reported (D-70); nothing is inferred in the browser.

export function conditionExplanation(s: LicenceSummary): string {
  switch (s.condition) {
    case LicenseCondition.SETUP:
      return "No licence is in force. Calls cannot be made or received until a licence is applied.";
    case LicenseCondition.DEGRADED:
      return `Capacity has fallen to ${s.channels} channels. ${reasonText(s.reason) ?? ""} Apply a valid licence to restore full capacity.`.replace("  ", " ");
    case LicenseCondition.CAPACITY_REACHED:
      return `All ${s.channels} channels are in use. New calls are being declined; calls in progress continue.`;
    case LicenseCondition.ENTITLEMENT_UNVERIFIED:
      return "The licence could not be confirmed recently. Nothing is restricted yet. Check that this server can reach the licence service.";
    case LicenseCondition.CAPACITY_WARNING:
      return `${s.channelsInUse} of ${s.channels} channels are in use. New calls will be declined when all are in use.`;
    case LicenseCondition.FREE_COMMUNITY:
      return `The Free Community licence is in force, with ${s.channels} channels.`;
    case LicenseCondition.VALID:
      return "The licence is in force.";
    default:
      return "The licence state could not be read.";
  }
}

const reasons: Record<string, string> = {
  not_activated: "No licence has been applied yet.",
  expired: "The licence expired, or could not be confirmed within its grace period.",
  tampered: "The stored licence failed verification.",
};

// reasonText words a server reason code, or returns null for one the
// console does not know — an unknown code is never shown raw.
export function reasonText(code: string): string | null {
  return reasons[code] ?? null;
}

export const tierLabel: Record<LicenseTier, string> = {
  [LicenseTier.UNSPECIFIED]: "None",
  [LicenseTier.FREE_COMMUNITY]: "Free Community",
  [LicenseTier.COMMERCIAL]: "Commercial",
};

export const capacityLabel: Record<CapacityLevel, string> = {
  [CapacityLevel.UNSPECIFIED]: "Not applicable",
  [CapacityLevel.NORMAL]: "Normal",
  [CapacityLevel.WARNING]: "Warning (80% or more in use)",
  [CapacityLevel.REACHED]: "Reached",
};

export const stateLabel: Record<LicenseState, string> = {
  [LicenseState.UNSPECIFIED]: "Unknown",
  [LicenseState.NOT_ACTIVATED]: "Not activated",
  [LicenseState.VALID]: "Valid",
  [LicenseState.UNVERIFIED]: "Unverified",
  [LicenseState.DEGRADED]: "Degraded",
};
