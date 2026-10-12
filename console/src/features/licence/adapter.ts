import { timestampDate, type Timestamp } from "@bufbuild/protobuf/wkt";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { useApiClients } from "../../api/ApiProvider";
import { callApi, type ApiError } from "../../api/errors";
import {
  CapacityLevel,
  LicenseCondition,
  LicenseState,
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
  state: LicenseState;
  // reason is the server's code for a state other than valid
  // ("not_activated", "expired", "tampered"); the page words it and never
  // shows a code it does not know.
  reason: string;
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
    state: status?.state ?? LicenseState.UNSPECIFIED,
    reason: status?.reason ?? "",
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

// STATUS_REFRESH_MS keeps channels in use and the capacity condition
// current while a page is open.
const STATUS_REFRESH_MS = 30_000;

export function useLicenceStatus() {
  const { licensing } = useApiClients();
  return useQuery<LicenceSummary, ApiError>({
    queryKey: licenceKeys.status(),
    queryFn: () => callApi(async () => toLicenceSummary((await licensing.getLicenseStatus({})).status)),
    refetchInterval: STATUS_REFRESH_MS,
  });
}

// useApplyLicence applies a licence key. The key goes to the server and
// nowhere else. TanStack Query holds a mutation's variables in memory, so
// the mutation is reset as soon as it settles and is never kept in the
// cache (gcTime 0): the key does not outlive the call. The status the
// server returns replaces the cached one, and the query is refetched to be
// sure — the console never assumes what a key will do (HLD-19 §5).
export function useApplyLicence() {
  const { licensing } = useApiClients();
  const queryClient = useQueryClient();
  const mutation = useMutation<LicenceSummary, ApiError, string>({
    mutationFn: (token) => callApi(async () => toLicenceSummary((await licensing.applyLicenseKey({ token })).status)),
    gcTime: 0,
    onSuccess: async (summary) => {
      queryClient.setQueryData(licenceKeys.status(), summary);
      await queryClient.invalidateQueries({ queryKey: licenceKeys.status() });
    },
  });
  const { mutateAsync, reset } = mutation;
  // apply resolves with the new status, or rejects with an ApiError, and
  // in both cases forgets the key.
  const apply = useCallback(
    async (token: string): Promise<LicenceSummary> => {
      try {
        return await mutateAsync(token);
      } finally {
        reset();
      }
    },
    [mutateAsync, reset],
  );
  return { apply, pending: mutation.isPending };
}
