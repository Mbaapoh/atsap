import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
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
  directory: (tenantId: string) => [...extensionKeys.all(tenantId), "directory"] as const,
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

// ExtensionEntry is how other screens name an extension: by number and
// display name, never by its id or SIP username.
export interface ExtensionEntry {
  id: string;
  number: string;
  name: string;
}

// DIRECTORY_PAGE is the server's largest page (pbx rpc maxPageLimit).
const DIRECTORY_PAGE = 200;

// useExtensionDirectory reads every extension of the tenant, page by page,
// for screens that must name or offer any of them (inbound numbers). It
// lives under the extensions key, so any extension change refreshes it.
export function useExtensionDirectory() {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<ExtensionEntry[], ApiError>({
    queryKey: extensionKeys.directory(tenantId),
    queryFn: () =>
      callApi(async () => {
        const out: ExtensionEntry[] = [];
        for (let offset = 0; ; offset += DIRECTORY_PAGE) {
          const { extensions } = await pbx.listExtensions({ tenantId, limit: DIRECTORY_PAGE, offset });
          out.push(...extensions.map((e) => ({ id: e.extensionId, number: e.number, name: e.displayName })));
          if (extensions.length < DIRECTORY_PAGE) return out;
        }
      }),
    enabled: tenantId !== "",
  });
}

// Registrar is where phones register (pbx-sip-registrar-settings), or
// null when the installer has not configured a public address.
export interface Registrar {
  host: string;
  port: number;
  transport: string;
}

export function useRegistrar() {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<Registrar | null, ApiError>({
    queryKey: ["sip-registrar", tenantId],
    queryFn: () =>
      callApi(async () => {
        const r = await pbx.getSipRegistrar({ tenantId });
        return r.configured ? { host: r.host, port: r.port, transport: r.transport } : null;
      }),
    enabled: tenantId !== "",
    staleTime: 5 * 60_000,
  });
}

// Credentials are what a phone needs, shown once (console brief §6 rule 8).
export interface Credentials {
  extension: ExtensionRow;
  secret: string;
}

export interface NewExtension {
  number: string;
  name: string;
  device: DeviceType;
}

// useCreateExtension creates an extension and returns its credentials
// once. The mutation is reset as soon as it settles and never cached, so
// the secret lives only in the caller's panel state.
export function useCreateExtension() {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  const queryClient = useQueryClient();
  const mutation = useMutation<Credentials, ApiError, NewExtension>({
    mutationFn: (e) =>
      callApi(async () => {
        const res = await pbx.createExtension({ tenantId, number: e.number, displayName: e.name, deviceType: e.device });
        if (!res.extension) throw new Error("no extension returned");
        return { extension: toExtensionRow(res.extension), secret: res.secret };
      }),
    gcTime: 0,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: extensionKeys.all(tenantId) }),
  });
  const { mutateAsync, reset } = mutation;
  const create = useCallback(
    async (e: NewExtension) => {
      try {
        return await mutateAsync(e);
      } finally {
        reset();
      }
    },
    [mutateAsync, reset],
  );
  return { create };
}

// useExtensionChanges edits, rotates and deletes. Every change is
// confirmed in the UI first and the list is refetched afterwards; nothing
// is optimistic. A rotated secret is returned once, never cached.
export function useExtensionChanges() {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  const queryClient = useQueryClient();
  const refetch = () => queryClient.invalidateQueries({ queryKey: extensionKeys.all(tenantId) });
  const update = useMutation<void, ApiError, { id: string; name: string; device: DeviceType }>({
    mutationFn: ({ id, name, device }) =>
      callApi(async () => {
        await pbx.updateExtension({ tenantId, extensionId: id, displayName: name, deviceType: device });
      }),
    onSuccess: refetch,
  });
  const remove = useMutation<void, ApiError, string>({
    mutationFn: (id) =>
      callApi(async () => {
        await pbx.deleteExtension({ tenantId, extensionId: id });
      }),
    onSuccess: refetch,
  });
  const rotation = useMutation<Credentials, ApiError, string>({
    mutationFn: (id) =>
      callApi(async () => {
        const res = await pbx.regenerateSecret({ tenantId, extensionId: id });
        if (!res.extension) throw new Error("no extension returned");
        return { extension: toExtensionRow(res.extension), secret: res.secret };
      }),
    gcTime: 0,
    onSuccess: refetch,
  });
  const { mutateAsync: rotateAsync, reset: resetRotation } = rotation;
  const rotate = useCallback(
    async (id: string) => {
      try {
        return await rotateAsync(id);
      } finally {
        resetRotation();
      }
    },
    [rotateAsync, resetRotation],
  );
  return { update: update.mutateAsync, remove: remove.mutateAsync, rotate };
}
