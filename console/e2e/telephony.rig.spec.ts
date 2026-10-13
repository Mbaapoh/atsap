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

test("A4: add, replace and delete outbound routes", async ({ page, request }) => {
  user = seedUser(["Acme"]);
  const tenantId = user.tenants[0]!.id;
  const token = ((await (await request.post("/atsapbx.v1.AuthService/SignIn", { data: { email: user.email, password: FIXTURE_PASSWORD } })).json()) as { access: { token: string } }).access.token;
  teardown = () => cleanTelephony(request, token, tenantId);
  for (const name of ["Carrier R1", "Carrier R2"]) {
    expect((await api(request, token, "CreateTrunk", { tenantId, name, host: "198.51.100.30", channelLimit: 2 })).status).toBe(200);
  }
  const trunkIds = Object.fromEntries(((await api(request, token, "ListTrunks", { tenantId })).body.trunks as { name: string; trunkId: string }[]).map((t) => [t.name, t.trunkId]));
  const listed = async () =>
    (((await api(request, token, "ListRoutes", { tenantId })).body.routes ?? []) as Record<string, unknown>[])
      .map((r) => ({ prefix: r.prefixPattern, trunk: r.trunkId, cost: r.costPerMinuteMicros ?? "0", priority: r.priority ?? 0 }))
      .sort((a, b) => String(a.prefix).localeCompare(String(b.prefix)));

  await signIn(page, user.email);
  await page.goto("/trunks");
  const table = page.getByRole("table", { name: "Outbound routes of this tenant" });
  for (const [prefix, cost] of [["+237", "0.015"], ["+2376", ""]] as const) {
    await page.getByRole("button", { name: "Add route" }).click();
    const dialog = page.getByRole("dialog", { name: "Add an outbound route" });
    await dialog.getByLabel("Prefix").fill(prefix);
    await dialog.getByLabel("Cost per minute (optional)").fill(cost);
    if (prefix === "+2376") await expect(dialog.getByText(/overlaps \+237 \(Carrier R1\)/)).toBeVisible();
    await dialog.getByRole("button", { name: "Add route" }).click();
    await expect(page.getByText(`Route ${prefix} added.`)).toBeVisible();
  }
  await expect(table.getByRole("row", { name: /\+237 Carrier R1/ })).toContainText("0.015");
  expect(await listed()).toEqual([
    { prefix: "+237", trunk: trunkIds["Carrier R1"], cost: "15000", priority: 0 },
    { prefix: "+2376", trunk: trunkIds["Carrier R1"], cost: "0", priority: 0 },
  ]);

  // Replace +2376 onto the second carrier: new route first, then the old one goes.
  await page.getByRole("button", { name: "Actions for route +2376" }).click();
  await page.getByRole("menuitem", { name: "Replace…" }).click();
  await page.getByRole("dialog").getByLabel("Trunk").selectOption({ label: "Carrier R2" });
  await page.getByRole("dialog").getByLabel("Priority").fill("2");
  await page.getByRole("dialog").getByRole("button", { name: "Continue" }).click();
  await page.getByRole("dialog", { name: "Replace route +2376?" }).getByRole("button", { name: "Replace route" }).click();
  await expect(page.getByText("Route +2376 replaced.")).toBeVisible();
  await expect(table.getByRole("row", { name: /\+2376 Carrier R2/ })).toBeVisible();
  expect(await listed()).toEqual([
    { prefix: "+237", trunk: trunkIds["Carrier R1"], cost: "15000", priority: 0 },
    { prefix: "+2376", trunk: trunkIds["Carrier R2"], cost: "0", priority: 2 },
  ]);

  await page.getByRole("button", { name: "Actions for route +237", exact: true }).click();
  await page.getByRole("menuitem", { name: "Delete route…" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Delete route" }).click();
  await expect(page.getByText("Route +237 deleted.")).toBeVisible();
  expect((await listed()).map((r) => r.prefix)).toEqual(["+2376"]);
});

test("A4: assign an inbound number to an extension and remove it", async ({ page, request }) => {
  user = seedUser(["Acme"]);
  const tenantId = user.tenants[0]!.id;
  const token = ((await (await request.post("/atsapbx.v1.AuthService/SignIn", { data: { email: user.email, password: FIXTURE_PASSWORD } })).json()) as { access: { token: string } }).access.token;
  const number = `77${String(Date.now()).slice(-4)}`;
  const created = await api(request, token, "CreateExtension", { tenantId, number, displayName: "Inbound e2e", deviceType: "DEVICE_TYPE_SIP" });
  expect(created, JSON.stringify(created.body)).toMatchObject({ status: 200 });
  const extensionId = created.body.extension.extensionId as string;
  teardown = async () => {
    for (const n of (await api(request, token, "ListInboundNumbers", { tenantId })).body.numbers ?? []) {
      await api(request, token, "DeleteInboundNumber", { tenantId, inboundNumberId: n.inboundNumberId });
    }
    await api(request, token, "DeleteExtension", { tenantId, extensionId });
  };
  const did = `+237699${String(Date.now()).slice(-6)}`;

  await signIn(page, user.email);
  await page.goto("/trunks");
  await page.getByRole("button", { name: "Assign number" }).click();
  const dialog = page.getByRole("dialog", { name: "Assign an inbound number" });
  await dialog.getByLabel("Number").fill(did);
  await dialog.getByLabel("Rings extension").selectOption({ label: `${number} · Inbound e2e` });
  await dialog.getByRole("button", { name: "Assign number" }).click();
  await expect(page.getByText(`${did} now rings ${number} · Inbound e2e.`)).toBeVisible();

  // Stored as digits; the leading + is not significant.
  const stored = did.slice(1);
  const table = page.getByRole("table", { name: "Inbound numbers of this tenant" });
  await expect(table.getByRole("row", { name: new RegExp(`${stored} ${number} · Inbound e2e`) })).toBeVisible();
  expect((await api(request, token, "ListInboundNumbers", { tenantId })).body.numbers).toEqual([expect.objectContaining({ number: stored, extensionId })]);

  // The same number again, written without +, is the same number.
  await page.getByRole("button", { name: "Assign number" }).click();
  await dialog.getByLabel("Number").fill(stored);
  await dialog.getByRole("button", { name: "Assign number" }).click();
  await expect(dialog.getByText("This number is already assigned in this tenant. Remove it first to move it.")).toBeVisible();
  await dialog.getByRole("button", { name: "Cancel" }).click();

  await page.getByRole("button", { name: `Actions for number ${stored}` }).click();
  await page.getByRole("menuitem", { name: "Remove number…" }).click();
  await expect(page.getByRole("dialog")).toContainText(`stop ringing extension ${number} · Inbound e2e`);
  await page.getByRole("dialog").getByRole("button", { name: "Remove number" }).click();
  await expect(page.getByText(`${stored} removed.`)).toBeVisible();
  expect((await api(request, token, "ListInboundNumbers", { tenantId })).body.numbers ?? []).toEqual([]);
});
