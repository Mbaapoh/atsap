import { expect, test, type APIRequestContext, type Page } from "@playwright/test";
import { engineAuthFingerprint, FIXTURE_PASSWORD, rigAvailable, seedUser, trunkEndpointId, type RigUser } from "./support/rig";

// console-admin-screens: HLD-18 A4 (trunks, routes, inbound, emergency)
// against the real API on the dev rig.

let rig = false;
test.beforeAll(async () => {
  rig = await rigAvailable();
});
test.beforeEach(() => {
  test.skip(!rig, "the dev rig is not running (make tilt-up)");
});

let user: RigUser | undefined;
let teardown: (() => Promise<void>) | undefined;
test.afterEach(async () => {
  await teardown?.();
  teardown = undefined;
  user?.cleanup();
  user = undefined;
});

export async function signIn(page: Page, email: string) {
  await page.goto("/sign-in");
  await page.getByLabel("Email").fill(email);
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByLabel("Password", { exact: true }).fill(FIXTURE_PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
}

export async function api(request: APIRequestContext, token: string, procedure: string, data: unknown) {
  const res = await request.post(`/atsapbx.v1.PbxService/${procedure}`, { data, headers: { Authorization: `Bearer ${token}` } });
  return { status: res.status(), body: (await res.json().catch(() => ({}))) as Record<string, any> };
}

export async function cleanTelephony(request: APIRequestContext, token: string, tenantId: string) {
  const routes = await api(request, token, "ListRoutes", { tenantId });
  for (const r of routes.body.routes ?? []) await api(request, token, "DeleteRoute", { tenantId, routeId: r.routeId });
  const trunks = await api(request, token, "ListTrunks", { tenantId });
  for (const t of trunks.body.trunks ?? []) await api(request, token, "DeleteTrunk", { tenantId, trunkId: t.trunkId });
}

test("A4: add, edit (secret kept) and delete a trunk, with its routes", async ({ page, request }) => {
  user = seedUser(["Acme"]);
  const tenantId = user.tenants[0]!.id;
  const token = ((await (await request.post("/atsapbx.v1.AuthService/SignIn", { data: { email: user.email, password: FIXTURE_PASSWORD } })).json()) as { access: { token: string } }).access.token;
  teardown = () => cleanTelephony(request, token, tenantId);
  const secret = `trunk-secret-${Date.now()}`;

  await signIn(page, user.email);
  await page.goto("/trunks");
  await page.getByRole("button", { name: "Add trunk" }).click();
  const dialog = page.getByRole("dialog", { name: "Add a trunk" });
  await dialog.getByLabel("Name", { exact: true }).fill("Carrier e2e");
  await dialog.getByLabel("Host").fill("198.51.100.20");
  await dialog.getByLabel("Channel limit").fill("4");
  await dialog.getByLabel("Username (optional)").fill("acct-e2e");
  await dialog.getByLabel("Secret (optional)").fill(secret);
  await dialog.getByRole("button", { name: "Add trunk" }).click();
  const row = page.getByRole("table", { name: "Trunks of this tenant" }).getByRole("row", { name: /Carrier e2e/ });
  await expect(row).toContainText("Username and secret");
  expect(await page.content()).not.toContain(secret);

  const trunkId = (await api(request, token, "ListTrunks", { tenantId })).body.trunks[0].trunkId as string;
  const endpoint = trunkEndpointId(trunkId);
  const stored = engineAuthFingerprint(endpoint);
  expect(stored).not.toBe("");

  // Edit with the secret left empty: the engine's credential is unchanged.
  await page.getByRole("button", { name: "Actions for trunk Carrier e2e" }).click();
  await page.getByRole("menuitem", { name: "Edit…" }).click();
  await page.getByRole("dialog").getByLabel("Channel limit").fill("6");
  await page.getByRole("dialog").getByRole("button", { name: "Save changes" }).click();
  await expect(row).toContainText("6");
  expect(engineAuthFingerprint(endpoint)).toBe(stored);

  // A route on it, then delete the trunk: route and engine state go.
  expect((await api(request, token, "CreateRoute", { tenantId, prefixPattern: "+237", trunkId, priority: 1 })).status).toBe(200);
  await page.getByRole("button", { name: "Actions for trunk Carrier e2e" }).click();
  await page.getByRole("menuitem", { name: "Delete trunk…" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Delete trunk" }).click();
  await expect(page.getByText("Trunk Carrier e2e deleted.")).toBeVisible();
  expect((await api(request, token, "ListRoutes", { tenantId })).body.routes ?? []).toEqual([]);
  expect(engineAuthFingerprint(endpoint)).toBe("");
});
