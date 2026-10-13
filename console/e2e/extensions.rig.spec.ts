import { expect, test, type APIRequestContext } from "@playwright/test";
import { engineFindings, renderedSurface } from "./support/exposure";
import { FIXTURE_PASSWORD, rigAvailable, seedUser, type RigUser } from "./support/rig";

// console-admin-screens: HLD-18 A3 (extensions) against the real API.

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

export async function apiToken(request: APIRequestContext, email: string): Promise<string> {
  const res = await request.post("/atsapbx.v1.AuthService/SignIn", { data: { email, password: FIXTURE_PASSWORD } });
  return ((await res.json()) as { access: { token: string } }).access.token;
}

test("A3: the list shows a real extension's observed registration, and no SIP username", async ({ page, request }) => {
  user = seedUser(["Acme"]);
  const tenantId = user.tenants[0]!.id;
  const token = await apiToken(request, user.email);
  const number = `76${String(Date.now()).slice(-4)}`;
  const res = await request.post("/atsapbx.v1.PbxService/CreateExtension", {
    data: { tenantId, number, displayName: "List e2e", deviceType: "DEVICE_TYPE_SIP" },
    headers: { Authorization: `Bearer ${token}` },
  });
  expect(res.status()).toBe(200);
  const extensionId = ((await res.json()) as { extension: { extensionId: string } }).extension.extensionId;
  teardown = async () => {
    await request.post("/atsapbx.v1.PbxService/DeleteExtension", { data: { tenantId, extensionId }, headers: { Authorization: `Bearer ${token}` } });
  };

  await page.goto("/sign-in");
  await page.getByLabel("Email").fill(user.email);
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByLabel("Password", { exact: true }).fill(FIXTURE_PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Extensions", exact: true }).click();
  const row = page.getByRole("table", { name: "Extensions of this tenant" }).getByRole("row", { name: new RegExp(number) });
  await expect(row).toContainText("List e2e");
  await expect(row).toContainText("Not registered");
  await expect(row).toContainText("Desk or soft phone (SIP)");
  expect(engineFindings("/extensions", await renderedSurface(page))).toEqual([]);
});
