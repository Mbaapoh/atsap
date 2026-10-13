import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { useApiClients } from "../../api/ApiProvider";
import { callApi, type ApiError } from "../../api/errors";
import { DeviceType, RegistrationStatus, type Extension } from "../../api/gen/atsapbx/v1/pbx_pb";
import { PAGE_SIZE } from "../../components/Pager";
import { useSession } from "../auth/adapter";

// The extensions feature adapter (HLD-18 A3). The SIP username is kept in
// the view model only for the one-time credential panel and the
// extension's detail, where it is shown labelled (console-admin-screens
// D3); lists never render it.

export type Registration = "registered" | "not-registered" | "unknown";

export interface ExtensionRow {
  id: string;
  number: string;
  name: string;
  device: DeviceType;
  registration: Registration;
  sipUsername: string;
}

function registrationOf(r: RegistrationStatus): Registration {
  if (r === RegistrationStatus.REGISTERED) return "registered";
  if (r === RegistrationStatus.NOT_REGISTERED) return "not-registered";
  return "unknown";
}

export function toExtensionRow(e: Extension): ExtensionRow {
  return {
    id: e.extensionId,
    number: e.number,
    name: e.displayName,
    device: e.deviceType,
    registration: registrationOf(e.registration),
    sipUsername: e.authUsername,
  };
}

export const extensionKeys = {
  all: (tenantId: string) => ["extensions", tenantId] as const,
  page: (tenantId: string, page: number) => [...extensionKeys.all(tenantId), "page", page] as const,
};

export function useTenantId(): string {
  return useSession().access?.tenantId ?? "";
}

// REGISTRATION_REFRESH_MS: registration is live device state, so the list
// re-reads it while open.
export const REGISTRATION_REFRESH_MS = 15_000;

export function useExtensions(page: number) {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<ExtensionRow[], ApiError>({
    queryKey: extensionKeys.page(tenantId, page),
    queryFn: () =>
      callApi(async () => (await pbx.listExtensions({ tenantId, limit: PAGE_SIZE, offset: page * PAGE_SIZE })).extensions.map(toExtensionRow)),
    enabled: tenantId !== "",
    placeholderData: keepPreviousData,
    refetchInterval: REGISTRATION_REFRESH_MS,
  });
}
