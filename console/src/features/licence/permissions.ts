import { usePermissions } from "../auth/permissions";

// useCanApplyLicence decides whether to show Apply licence: only a
// platform administrator may apply one (ApplyLicenseKey requires system
// scope). Showing is not allowing: the server checks the scope on every
// call (console brief §6 rule 6).
export function useCanApplyLicence(): boolean {
  return usePermissions().isPlatformAdmin;
}
