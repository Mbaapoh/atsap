import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback } from "react";
import { useApiClients } from "../../api/ApiProvider";
import { callApi, type ApiError } from "../../api/errors";
import { PrincipalStatus } from "../../api/gen/atsapbx/v1/identity_pb";
import { create as createMessage } from "@bufbuild/protobuf";
import {
  AssignExtensionRequestSchema,
  DeviceType,
  OutboundClass,
  RegistrationStatus,
  UnassignExtensionRequestSchema,
  type Extension,
} from "../../api/gen/atsapbx/v1/pbx_pb";
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
  outbound: OutboundClass;
  registration: Registration;
  sipUsername: string;
  assignedPrincipalId: string;
  assignedPrincipalName: string;
  assignedPrincipalEmail: string;
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
    outbound: e.outboundClass === OutboundClass.UNSPECIFIED ? OutboundClass.NATIONAL : e.outboundClass,
    registration: registrationOf(e.registration),
    sipUsername: e.authUsername,
    assignedPrincipalId: e.assignedPrincipalId,
    assignedPrincipalName: e.assignedPrincipalName,
    assignedPrincipalEmail: e.assignedPrincipalEmail,
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
  // registration is the platform's observation, not configuration: a
  // call can only be placed from an extension that has one.
  registration: Registration;
  // device is how a device attaches: the browser phone offers WEBRTC
  // extensions only.
  device: DeviceType;
  outbound: OutboundClass;
  // sipUsername is the engine endpoint id the browser phone registers
  // with. It is used by the softphone, never rendered outside the marked
  // credential surface (console brief §6 rule 8).
  sipUsername: string;
}

// DIRECTORY_PAGE is the server's largest page (pbx rpc maxPageLimit).
const DIRECTORY_PAGE = 200;

// useExtensionDirectory reads every extension of the tenant, page by page,
// for screens that must name or offer any of them (inbound numbers). It
// lives under the extensions key, so any extension change refreshes it.
export function useExtensionDirectory(enabled = true) {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<ExtensionEntry[], ApiError>({
    queryKey: extensionKeys.directory(tenantId),
    queryFn: () =>
      callApi(async () => {
        const out: ExtensionEntry[] = [];
        for (let offset = 0; ; offset += DIRECTORY_PAGE) {
          const { extensions } = await pbx.listExtensions({ tenantId, limit: DIRECTORY_PAGE, offset });
          out.push(
            ...extensions.map((e) => ({
              id: e.extensionId,
              number: e.number,
              name: e.displayName,
              registration: registrationOf(e.registration),
              device: e.deviceType,
              outbound: e.outboundClass === OutboundClass.UNSPECIFIED ? OutboundClass.NATIONAL : e.outboundClass,
              sipUsername: e.authUsername,
            })),
          );
          if (extensions.length < DIRECTORY_PAGE) return out;
        }
      }),
    enabled: tenantId !== "" && enabled,
  });
}

// Registrar is where phones register (pbx-sip-registrar-settings), or
// null when the installer has not configured a public address.
export interface Registrar {
  host: string;
  port: number;
  transport: string;
  // webrtcUrl is the browser phone's signalling endpoint, empty when the
  // installation has not configured one (D-81).
  webrtcUrl: string;
  // iceServers are STUN/TURN servers for browser media (D-84).
  iceServers: RTCIceServer[];
}

export function useRegistrar() {
  const { pbx } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<Registrar | null, ApiError>({
    queryKey: ["sip-registrar", tenantId],
    queryFn: () =>
      callApi(async () => {
        const r = await pbx.getSipRegistrar({ tenantId });
        return r.configured
          ? {
              host: r.host,
              port: r.port,
              transport: r.transport,
              webrtcUrl: r.webrtcUrl,
              iceServers: r.iceServers
                .filter((s) => s.urls.length > 0)
                .map((s) => ({
                  urls: s.urls.length === 1 ? (s.urls[0] ?? "") : [...s.urls],
                  username: s.username || undefined,
                  credential: s.credential || undefined,
                })),
            }
          : null;
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
  outbound: OutboundClass;
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
        const res = await pbx.createExtension({ tenantId, number: e.number, displayName: e.name, deviceType: e.device, outboundClass: e.outbound });
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
  const update = useMutation<void, ApiError, { id: string; name: string; device: DeviceType; outbound: OutboundClass }>({
    mutationFn: ({ id, name, device, outbound }) =>
      callApi(async () => {
        await pbx.updateExtension({ tenantId, extensionId: id, displayName: name, deviceType: device, outboundClass: outbound });
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
  const assign = useMutation<void, ApiError, { id: string; principalId: string }>({
    mutationFn: ({ id, principalId }) =>
      callApi(async () => {
        await pbx.assignExtension(createMessage(AssignExtensionRequestSchema, { tenantId, extensionId: id, principalId }));
      }),
    onSuccess: refetch,
  });
  const unassign = useMutation<void, ApiError, string>({
    mutationFn: (id) =>
      callApi(async () => {
        await pbx.unassignExtension(createMessage(UnassignExtensionRequestSchema, { tenantId, extensionId: id }));
      }),
    onSuccess: refetch,
  });
  return { update: update.mutateAsync, remove: remove.mutateAsync, rotate, assign: assign.mutateAsync, unassign: unassign.mutateAsync };
}

// AssignablePrincipal is one active membership an extension may be
// assigned to.
export interface AssignablePrincipal {
  id: string;
  name: string;
  email: string;
}

export function useAssignablePrincipals() {
  const { identity } = useApiClients();
  const tenantId = useTenantId();
  return useQuery<AssignablePrincipal[], ApiError>({
    queryKey: [...extensionKeys.all(tenantId), "assignable"],
    queryFn: () =>
      callApi(async () => {
        const { principals } = await identity.listPrincipals({ tenantId, limit: 200, offset: 0 });
        const out: AssignablePrincipal[] = [];
        for (const p of principals) {
          const principal = p.principal;
          if (principal && principal.status === PrincipalStatus.ACTIVE) {
            out.push({ id: principal.principalId, name: principal.displayName, email: principal.email });
          }
        }
        return out;
      }),
    enabled: tenantId !== "",
  });
}
