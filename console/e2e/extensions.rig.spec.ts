import { expect, test, type APIRequestContext } from "@playwright/test";
import { engineFindings, renderedSurface } from "./support/exposure";
import { engineCredential, FIXTURE_PASSWORD, rigAvailable, seedUser, type RigUser } from "./support/rig";

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

test("A3: create an extension and receive its credentials once, with the registrar address", async ({ page, request }) => {
  user = seedUser(["Acme"]);
  const tenantId = user.tenants[0]!.id;
  const token = await apiToken(request, user.email);
  const number = `75${String(Date.now()).slice(-4)}`;
  teardown = async () => {
    const listed = await request.post("/atsapbx.v1.PbxService/ListExtensions", { data: { tenantId }, headers: { Authorization: `Bearer ${token}` } });
    for (const e of ((await listed.json()) as { extensions?: { extensionId: string }[] }).extensions ?? []) {
      await request.post("/atsapbx.v1.PbxService/DeleteExtension", { data: { tenantId, extensionId: e.extensionId }, headers: { Authorization: `Bearer ${token}` } });
    }
  };

  await page.goto("/sign-in");
  await page.getByLabel("Email").fill(user.email);
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByLabel("Password", { exact: true }).fill(FIXTURE_PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
  await page.goto("/extensions");
  await page.getByRole("button", { name: "Add extension" }).click();
  const dialog = page.getByRole("dialog", { name: "Add an extension" });
  await dialog.getByLabel("Number").fill(number);
  await dialog.getByLabel("Name").fill("Created e2e");
  await dialog.getByRole("button", { name: "Add extension" }).click();

  const panel = page.getByRole("dialog", { name: `Extension ${number} created` });
  await expect(panel).toContainText("localhost");
  await expect(panel).toContainText("5060");
  await expect(panel).toContainText("UDP");
  const username = (await panel.locator("[data-sip-username]").textContent())!.trim();
  expect(username).toMatch(/^e_[0-9a-f]{32}$/);
  const secret = (await panel.locator(".secret-value").last().textContent())!.trim();
  expect(secret.length).toBeGreaterThanOrEqual(20);
  // While shown, the only engine id on screen is the marked SIP username.
  expect(engineFindings("credential panel", await renderedSurface(page))).toEqual([]);

  await panel.getByRole("checkbox", { name: "I have saved these details" }).check();
  await panel.getByRole("button", { name: "Done" }).click();
  await expect(page.getByRole("table", { name: "Extensions of this tenant" }).getByRole("row", { name: new RegExp(number) })).toContainText("Not registered");
  const after = await page.content();
  expect(after).not.toContain(secret);
  expect(after).not.toContain(username);
});

test("A3: edit, rotate (the engine's credential changes) and delete (the engine forgets it)", async ({ page, request }) => {
  user = seedUser(["Acme"]);
  const tenantId = user.tenants[0]!.id;
  const token = await apiToken(request, user.email);
  const number = `74${String(Date.now()).slice(-4)}`;
  const res = await request.post("/atsapbx.v1.PbxService/CreateExtension", {
    data: { tenantId, number, displayName: "Change e2e", deviceType: "DEVICE_TYPE_SIP" },
    headers: { Authorization: `Bearer ${token}` },
  });
  const created = (await res.json()) as { extension: { extensionId: string; authUsername: string } };
  let deleted = false;
  teardown = async () => {
    if (!deleted) {
      await request.post("/atsapbx.v1.PbxService/DeleteExtension", { data: { tenantId, extensionId: created.extension.extensionId }, headers: { Authorization: `Bearer ${token}` } });
    }
  };
  const endpoint = created.extension.authUsername;
  const before = engineCredential(endpoint);
  expect(before).not.toBe("");

  await page.goto("/sign-in");
  await page.getByLabel("Email").fill(user.email);
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByLabel("Password", { exact: true }).fill(FIXTURE_PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
  await page.goto("/extensions");
  const row = page.getByRole("table", { name: "Extensions of this tenant" }).getByRole("row", { name: new RegExp(number) });
  const actions = page.getByRole("button", { name: `Actions for extension ${number}` });

  // Edit.
  await actions.click();
  await page.getByRole("menuitem", { name: "Edit…" }).click();
  await page.getByRole("dialog").getByLabel("Name").fill("Renamed e2e");
  await page.getByRole("dialog").getByRole("button", { name: "Save changes" }).click();
  await expect(row).toContainText("Renamed e2e");

  // Rotate: a new password shown once, and the engine's credential changed.
  await actions.click();
  await page.getByRole("menuitem", { name: "Rotate SIP password…" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Rotate password" }).click();
  const panel = page.getByRole("dialog", { name: `New SIP password for extension ${number}` });
  const secret = (await panel.locator(".secret-value").last().textContent())!.trim();
  await panel.getByRole("checkbox", { name: "I have saved these details" }).check();
  await panel.getByRole("button", { name: "Done" }).click();
  expect(engineCredential(endpoint)).not.toBe(before);
  expect(await page.content()).not.toContain(secret);

  // Delete: gone from the list, and the engine no longer knows it.
  await actions.click();
  await page.getByRole("menuitem", { name: "Delete extension…" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Delete extension" }).click();
  await expect(page.getByText(`Extension ${number} deleted.`)).toBeVisible();
  deleted = true;
  await expect(row).toHaveCount(0);
  expect(engineCredential(endpoint)).toBe("");
});
