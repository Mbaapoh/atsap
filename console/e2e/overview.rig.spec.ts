import { expect, test, type APIRequestContext } from "@playwright/test";
import { FIXTURE_PASSWORD, rigAvailable, seedUser, type RigUser } from "./support/rig";

// console-foundation task 3.1 against the real API on the dev rig: the
// checklist reports the tenant's actual state, and changes when that state
// does — here, when an extension is created through the public API.

let rig = false;
test.beforeAll(async () => {
  rig = await rigAvailable();
});
test.beforeEach(() => {
  test.skip(!rig, "the dev rig is not running (make tilt-up)");
});

let user: RigUser | undefined;
test.afterEach(() => {
  user?.cleanup();
  user = undefined;
});

async function token(request: APIRequestContext, email: string): Promise<string> {
  const res = await request.post("/atsapbx.v1.AuthService/SignIn", { data: { email, password: FIXTURE_PASSWORD } });
  return ((await res.json()) as { access: { token: string } }).access.token;
}

test("the checklist shows the tenant's real state and follows it", async ({ page, request }) => {
  user = seedUser(["Acme"]);
  const tenantId = user.tenants[0]!.id;

  await page.goto("/sign-in");
  await page.getByLabel("Email").fill(user.email);
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByLabel("Password", { exact: true }).fill(FIXTURE_PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();

  const step = (title: string) => page.locator("li.checklist-item", { hasText: title });
  // The rig is licensed; a brand-new tenant has nothing else yet.
  await expect(step("Apply a licence")).toContainText("Done");
  await expect(step("Add a user")).toContainText("To do");
  await expect(step("Add an extension")).toContainText("To do");
  await expect(step("Connect a trunk and a route")).toContainText("To do");
  await expect(step("Complete a call")).toContainText("To do");
  await expect(page.getByText("No calls yet")).toBeVisible();

  // Create an extension through the public API, as the console will.
  const auth = { Authorization: `Bearer ${await token(request, user.email)}` };
  const number = `79${String(Date.now()).slice(-4)}`;
  const created = await request.post("/atsapbx.v1.PbxService/CreateExtension", {
    data: { tenantId, number, displayName: "Overview e2e", deviceType: "DEVICE_TYPE_SIP" },
    headers: auth,
  });
  expect(created.status(), await created.text()).toBe(200);
  const extensionId = ((await created.json()) as { extension: { extensionId: string } }).extension.extensionId;
  try {
    await page.reload();
    await expect(step("Add an extension")).toContainText("Done");
    await expect(step("Add a user")).toContainText("To do");
  } finally {
    const deleted = await request.post("/atsapbx.v1.PbxService/DeleteExtension", { data: { tenantId, extensionId }, headers: auth });
    expect(deleted.status()).toBe(200);
  }
});
