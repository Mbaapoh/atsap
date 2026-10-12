import { timestampDate, type Timestamp } from "@bufbuild/protobuf/wkt";
import { useQuery } from "@tanstack/react-query";
import { useApiClients } from "../../api/ApiProvider";
import { callApi, type ApiError } from "../../api/errors";
import {
  CapacityLevel,
  LicenseCondition,
  LicenseTier,
  type LicenseStatus,
} from "../../api/gen/atsapbx/v1/licensing_pb";

// The licence feature adapter: the only place licence screens reach the
// API. It wraps the generated client, turns failures into ApiError, and
// hands components a plain view model, so a component never sees a wire
// message, a transport or a raw error (HLD-19 §3).

// LicenceSummary is the licence state as the console shows it. The
// condition, tier and capacity level come from the server as reported
// (D-70); nothing here derives them.
export interface LicenceSummary {
  condition: LicenseCondition;
  tier: LicenseTier;
  capacity: CapacityLevel;
  edition: string;
  channels: number;
  channelsInUse: number;
  // null means unlimited.
  maxTenants: number | null;
  maxExtensions: number | null;
  // null when the licence does not expire or none is in force.
  expiresAt: Date | null;
  lastConfirmedAt: Date | null;
}

function dateOrNull(ts: Timestamp | undefined): Date | null {
  return ts ? timestampDate(ts) : null;
}

function limit(n: number): number | null {
  return n > 0 ? n : null;
}

// toLicenceSummary maps the wire status. A response without one reads as
// an unknown condition, never as a valid licence.
export function toLicenceSummary(status: LicenseStatus | undefined): LicenceSummary {
  return {
    condition: status?.condition ?? LicenseCondition.UNSPECIFIED,
    tier: status?.tier ?? LicenseTier.UNSPECIFIED,
    capacity: status?.capacity ?? CapacityLevel.UNSPECIFIED,
    edition: status?.edition ?? "",
    channels: status?.channels ?? 0,
    channelsInUse: status?.channelsInUse ?? 0,
    maxTenants: limit(status?.maxTenants ?? 0),
    maxExtensions: limit(status?.maxExtensions ?? 0),
    expiresAt: dateOrNull(status?.expiresAt),
    lastConfirmedAt: dateOrNull(status?.lastConfirmedAt),
  };
}

// licenceKeys are the query keys the licence feature owns; a mutation
// invalidates them instead of guessing the new state (HLD-19 §5).
export const licenceKeys = {
  all: ["licence"] as const,
  status: () => [...licenceKeys.all, "status"] as const,
};

export function useLicenceStatus() {
  const { licensing } = useApiClients();
  return useQuery<LicenceSummary, ApiError>({
    queryKey: licenceKeys.status(),
    queryFn: () => callApi(async () => toLicenceSummary((await licensing.getLicenseStatus({})).status)),
  });
}
