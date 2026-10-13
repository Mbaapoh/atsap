import { expect, test, type Page } from "@playwright/test";
import { mockEmptyTenant } from "./support/session";

// console-foundation task 3.2: HLD-18 Flow A1 from the console onward, in
// a real browser at desktop and phone widths, against a stateful mock of
// the public API. It covers what the dev rig cannot show — an installation
// in Setup — in one continuous journey: sign in, choose a Tenant, meet the
// Setup gate, apply a licence, see the gate lift, reload and stay signed
// in. The same journey against the real API, with the rig's real key, is
// e2e/uat/a1-first-login.uat.spec.ts.

const ACME = { tenantId: "11111111-1111-4111-8111-111111111111", tenantName: "Acme Telecommunications Cameroon", principalId: "p-a", roles: ["PLATFORM_ADMIN"] };
const GLOBEX = { tenantId: "22222222-2222-4222-8222-222222222222", tenantName: "Globex", principalId: "p-b", roles: ["TENANT_ADMIN"] };
const KEY = "eyJhbGciOiJFZERTQSJ9.YTEtam91cm5leS1rZXk.c2ln";

// Installation is the mocked server's state. The session is what the
// HttpOnly cookie would carry: a session exists, and which membership it
// last chose.
interface Installation {
  licensed: boolean;
  session: { tenantId: string | null } | null;
  applied: string[];
}

const json = (body: unknown, status = 200) => ({ status, contentType: "application/json", body: JSON.stringify(body) });

function grant(tenantId: string) {
  const m = tenantId === ACME.tenantId ? ACME : GLOBEX;
  return { token: `token-${m.principalId}`, tenantId: m.tenantId, principalId: m.principalId, expiresAt: "2030-01-01T00:00:00Z" };
}

async function mockInstallation(page: Page): Promise<Installation> {
  const state: Installation = { licensed: false, session: null, applied: [] };
  const memberships = [ACME, GLOBEX];
  const route = (proc: string, handler: (body: Record<string, unknown>) => ReturnType<typeof json>) =>
    page.route(`**/atsapbx.v1.${proc}`, async (r) => r.fulfill(handler((r.request().postDataJSON() ?? {}) as Record<string, unknown>)));

  await route("AuthService/DiscoverSignIn", () => json({ method: "SIGN_IN_METHOD_PASSWORD" }));
  await route("AuthService/SignIn", (b) => {
    if (b.password !== "correct horse battery") return json({ code: "unauthenticated", message: "x" }, 401);
    state.session = { tenantId: null };
    return json({ memberships });
  });
  await route("AuthService/RefreshSession", (b) => {
    if (!state.session) return json({ code: "unauthenticated", message: "x" }, 401);
    if (typeof b.tenantId === "string" && b.tenantId) state.session.tenantId = b.tenantId;
    const tenantId = state.session.tenantId;
    return json(tenantId ? { access: grant(tenantId), memberships } : { memberships });
  });
  await route("LicensingService/GetLicenseStatus", () =>
    json({
      status: state.licensed
        ? { state: "LICENSE_STATE_VALID", condition: "LICENSE_CONDITION_VALID", tier: "LICENSE_TIER_COMMERCIAL", capacity: "CAPACITY_LEVEL_NORMAL", edition: "standard", channels: 16 }
        : { state: "LICENSE_STATE_NOT_ACTIVATED", condition: "LICENSE_CONDITION_SETUP", reason: "not_activated" },
    }),
  );
  await route("LicensingService/ApplyLicenseKey", (b) => {
    state.applied.push(String(b.token));
    state.licensed = true;
    return json({ status: { state: "LICENSE_STATE_VALID", condition: "LICENSE_CONDITION_VALID", channels: 16 } });
  });
  await mockEmptyTenant(page);
  return state;
}

// openNav reaches the main navigation the way a person at this width
// does: the rail on desktop, the drawer behind the menu button on a phone.
async function go(page: Page, phone: boolean, name: string) {
  if (phone) {
    await page.getByRole("button", { name: "Open navigation" }).click();
    await page.getByRole("dialog").getByRole("link", { name, exact: true }).click();
  } else {
    await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name, exact: true }).click();
  }
}

for (const viewport of [
  { label: "desktop", width: 1280, height: 800, phone: false },
  { label: "phone", width: 390, height: 844, phone: true },
  { label: "small phone", width: 360, height: 740, phone: true },
]) {
  test.describe(viewport.label, () => {
    test.use({ viewport: { width: viewport.width, height: viewport.height } });

    test("A1: sign in, choose a tenant, meet the Setup gate, apply a licence, stay signed in", async ({ page }) => {
      const server = await mockInstallation(page);

      // Not signed in: the console opens on sign-in.
      await page.goto("/");
      await expect(page.getByRole("heading", { level: 1, name: "Sign in" })).toBeVisible();

      // A wrong password is answered neutrally; the right one signs in.
      await page.getByLabel("Email").fill("admin@example.com");
      await page.getByRole("button", { name: "Continue" }).click();
      await page.getByLabel("Password", { exact: true }).fill("wrong");
      await page.getByRole("button", { name: "Sign in" }).click();
      await expect(page.getByRole("alert")).toHaveText("The email or password is not correct.");
      await page.getByLabel("Password", { exact: true }).fill("correct horse battery");
      await page.getByRole("button", { name: "Sign in" }).click();

      // Two memberships: choose the tenant to work in.
      await expect(page.getByRole("heading", { level: 1, name: "Choose a tenant" })).toBeVisible();
      await page.getByRole("button", { name: /Acme/ }).click();
      await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
      await expect(page.locator(".tenant-indicator")).toContainText("Acme");

      // Setup: the pill says so, the checklist's first step is to do, and
      // telephony configuration leads to the licence page with the reason.
      await expect(page.getByRole("link", { name: /^Licence: Setup/ })).toBeVisible();
      await expect(page.locator("li.checklist-item", { hasText: "Apply a licence" })).toContainText("To do");
      await go(page, viewport.phone, "Extensions");
      await expect(page).toHaveURL(/\/licence$/);
      await expect(page.getByText("Extensions open once a licence is in force")).toBeVisible();

      // Apply the key: masked, confirmed, then the licence is in force.
      const key = page.getByLabel("Licence key");
      await key.fill(KEY);
      await expect(key).toHaveAttribute("type", "password");
      await page.getByRole("button", { name: "Apply licence" }).click();
      await page.getByRole("dialog", { name: "Apply this licence?" }).getByRole("button", { name: "Apply licence" }).click();
      await expect(page.getByText("Licence applied.")).toBeVisible();
      await expect(page.locator(".licence-condition")).toHaveText("Valid");
      expect(server.applied).toEqual([KEY]);
      expect(await page.content()).not.toContain(KEY);

      // The gate has lifted.
      await go(page, viewport.phone, "Extensions");
      await expect(page.getByRole("heading", { level: 1, name: "Extensions" })).toBeVisible();

      // Session recovery: a reload loses the in-memory token; the cookie
      // brings the session back, in the tenant chosen before.
      await page.reload();
      await expect(page.getByRole("heading", { level: 1, name: "Extensions" })).toBeVisible();
      await expect(page.locator(".tenant-indicator")).toContainText("Acme");
      await expect(page.getByRole("link", { name: /^Licence: Valid/ })).toBeVisible();

      // Nothing credential-bearing was kept by the page.
      const kept = await page.evaluate(() => localStorage.length + sessionStorage.length + document.cookie.length);
      expect(kept).toBe(0);
      if (viewport.phone) {
        const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
        expect(overflow).toBe(0);
      }
    });
  });
}
