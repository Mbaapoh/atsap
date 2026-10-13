import type { Page } from "@playwright/test";

// mockLicence answers GetLicenseStatus, which every signed-in page reads
// for the top-bar pill.
export async function mockLicence(page: Page, status: Record<string, unknown> = {}): Promise<void> {
  await page.route("**/atsapbx.v1.LicensingService/GetLicenseStatus", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        status: {
          state: "LICENSE_STATE_VALID",
          edition: "standard",
          channels: 16,
          channelsInUse: 2,
          condition: "LICENSE_CONDITION_VALID",
          tier: "LICENSE_TIER_COMMERCIAL",
          capacity: "CAPACITY_LEVEL_NORMAL",
          ...status,
        },
      }),
    }),
  );
}

// mockEmptyTenant answers the overview's reads for a new tenant: one
// user, and no extensions, trunks, routes or calls.
export async function mockEmptyTenant(page: Page): Promise<void> {
  const empty = { contentType: "application/json", body: "{}" };
  await page.route("**/atsapbx.v1.IdentityService/ListPrincipals", (r) => r.fulfill({ contentType: "application/json", body: JSON.stringify({ principals: [{}] }) }));
  for (const p of ["PbxService/ListExtensions", "PbxService/ListTrunks", "PbxService/ListRoutes", "TelephonyService/ListCalls"]) {
    await page.route(`**/atsapbx.v1.${p}`, (r) => r.fulfill(empty));
  }
}

// mockSession answers the console's on-load RefreshSession with a session
// for one Tenant (with roles), and the licence status with a valid
// licence, for specs about the signed-in shell rather than about signing
// in (auth.rig.spec.ts covers that against the real API).
export async function mockSession(page: Page, roles: string[] = ["TENANT_ADMIN"]): Promise<void> {
  await mockLicence(page);
  await mockEmptyTenant(page);
  await page.route("**/atsapbx.v1.AuthService/RefreshSession", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        access: { token: "e2e-mock-token", tenantId: "11111111-1111-4111-8111-111111111111", principalId: "p", expiresAt: "2030-01-01T00:00:00Z" },
        memberships: [{ tenantId: "11111111-1111-4111-8111-111111111111", tenantName: "Acme", principalId: "p", roles }],
      }),
    }),
  );
}
