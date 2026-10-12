import type { DescService } from "@bufbuild/protobuf";
import { AuthService } from "./gen/atsapbx/v1/auth_pb";
import { IdentityService } from "./gen/atsapbx/v1/identity_pb";
import { LicensingService } from "./gen/atsapbx/v1/licensing_pb";
import { PbxService } from "./gen/atsapbx/v1/pbx_pb";
import { TelephonyService } from "./gen/atsapbx/v1/telephony_pb";

// PUBLIC_SERVICES are the documented public API services the console
// consumes (docs/API.md). The console is not a privileged client (BRD §16,
// D-43): it reaches the server through these and nothing else — no health
// or readiness probe, no undocumented route.
export const PUBLIC_SERVICES: readonly DescService[] = [
  AuthService,
  IdentityService,
  LicensingService,
  PbxService,
  TelephonyService,
];

// PUBLIC_PROCEDURES are the request paths those services define, derived
// from the generated descriptors rather than written by hand, so the list
// follows the protos.
export const PUBLIC_PROCEDURES: ReadonlySet<string> = new Set(
  PUBLIC_SERVICES.flatMap((s) => s.methods.map((m) => `/${s.typeName}/${m.name}`)),
);

// isPublicProcedure reports whether url, resolved against origin, is a
// public procedure on that same origin.
export function isPublicProcedure(url: string, origin: string): boolean {
  let parsed: URL;
  try {
    parsed = new URL(url, origin);
  } catch {
    return false;
  }
  return parsed.origin === new URL(origin).origin && PUBLIC_PROCEDURES.has(parsed.pathname);
}
