import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { createApiClients } from "../../api/clients";
import { createApiTransport } from "../../api/transport";
import { signedIn, tenantA, tenantB, TEST_BASE } from "../../test/session";
import { SessionContext, SessionController } from "./adapter";
import { permissionsFor, roleLabel, ROLE_CATALOGUE, usePermissions } from "./permissions";
import { SessionStore, type SessionState } from "./session";

// console-admin-screens task 1.1: what each role is shown.

describe("permissionsFor", () => {
  it.each([
    [["AGENT"], false, false, []],
    [["SUPERVISOR"], false, false, []],
    [["TENANT_ADMIN"], true, false, ["AGENT", "SUPERVISOR", "TENANT_ADMIN"]],
    [["PLATFORM_ADMIN"], true, true, ["AGENT", "SUPERVISOR", "TENANT_ADMIN", "PLATFORM_ADMIN"]],
    [["AGENT", "TENANT_ADMIN"], true, false, ["AGENT", "SUPERVISOR", "TENANT_ADMIN"]],
    [[], false, false, []],
    [["SOMETHING_NEW"], false, false, []],
  ])("%j: administer %s, platform %s, may grant %j", (roles, administer, platform, grantable) => {
    const p = permissionsFor(roles);
    expect(p.canAdminister).toBe(administer);
    expect(p.isPlatformAdmin).toBe(platform);
    expect(p.grantableRoles.map((r) => r.role)).toEqual(grantable);
  });

  it("offers the platform-administrator role only to a platform administrator", () => {
    expect(permissionsFor(["TENANT_ADMIN"]).grantableRoles.some((r) => r.scope === "system")).toBe(false);
    expect(permissionsFor(["PLATFORM_ADMIN"]).grantableRoles.find((r) => r.role === "PLATFORM_ADMIN")?.scope).toBe("system");
  });
});

describe("roleLabel", () => {
  it("names every catalogued role in words", () => {
    for (const r of ROLE_CATALOGUE) expect(roleLabel(r.role)).not.toBe(r.role);
  });

  it("shows an unknown role by its server name, never hides or renames it", () => {
    expect(roleLabel("BILLING_CLERK")).toBe("BILLING_CLERK");
  });
});

describe("usePermissions", () => {
  function wrapperFor(state: SessionState) {
    const store = new SessionStore(state);
    const controller = new SessionController(createApiClients(createApiTransport({ baseUrl: TEST_BASE })).auth, store);
    return ({ children }: { children: ReactNode }) => <SessionContext.Provider value={controller}>{children}</SessionContext.Provider>;
  }

  it("reads the current membership, not every membership", () => {
    const state = signedIn({ memberships: [{ ...tenantA, roles: ["AGENT"] }, { ...tenantB, roles: ["TENANT_ADMIN"] }] });
    const { result } = renderHook(() => usePermissions(), { wrapper: wrapperFor(state) });
    expect(result.current.canAdminister).toBe(false);
    expect(result.current.roles).toEqual(["AGENT"]);
  });

  it("an administrator of the current tenant administers it", () => {
    const { result } = renderHook(() => usePermissions(), { wrapper: wrapperFor(signedIn()) });
    expect(result.current.canAdminister).toBe(true);
  });
});
