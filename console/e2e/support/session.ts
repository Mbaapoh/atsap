import type { Page } from "@playwright/test";

// mockSession answers the console's on-load RefreshSession with a session
// for one Tenant, for specs about the signed-in shell rather than about
// signing in (auth.rig.spec.ts covers that against the real API).
export async function mockSession(page: Page): Promise<void> {
  await page.route("**/atsapbx.v1.AuthService/RefreshSession", (route) =>
    route.fulfill({
      contentType: "application/json",
      body: JSON.stringify({
        access: { token: "e2e-mock-token", tenantId: "11111111-1111-4111-8111-111111111111", principalId: "p", expiresAt: "2030-01-01T00:00:00Z" },
        memberships: [{ tenantId: "11111111-1111-4111-8111-111111111111", tenantName: "Acme", principalId: "p", roles: ["TENANT_ADMIN"] }],
      }),
    }),
  );
}
