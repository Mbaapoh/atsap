import type { ReactNode } from "react";
import { Navigate } from "react-router";
import { LicenseCondition } from "../../api/gen/atsapbx/v1/licensing_pb";
import { useLicenceStatus } from "./adapter";

// RequireLicence is the Setup gate (console-foundation design, "Routes and
// guard"): while no licence is in force, telephony configuration pages
// send the person to the licence page with an explanation. Every other
// condition — Unverified, Capacity Warning or Reached, Degraded — stays
// administrable; the server enforces what is actually restricted. If the
// status cannot be read the page is shown, and the server decides.
export function RequireLicence({ area, children }: { area: string; children: ReactNode }) {
  const status = useLicenceStatus();
  if (status.isPending) return <p role="status">Checking the licence…</p>;
  if (status.data?.condition === LicenseCondition.SETUP) {
    return <Navigate to="/licence" replace state={{ blocked: area }} />;
  }
  return <>{children}</>;
}
