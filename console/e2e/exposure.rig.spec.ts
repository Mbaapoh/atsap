import { expect, test, type APIRequestContext } from "@playwright/test";
import { engineFindings, recordTraffic, renderFindings, renderedSurface, secretFindings, trafficFindings, type Finding } from "./support/exposure";
import { FIXTURE_PASSWORD, rigAvailable, seedCalls, seedUser, type RigUser } from "./support/rig";

// console-foundation task 3.3, extended to every admin screen by
// console-admin-screens task 6.3, against the real API on the dev rig:
// with engine-backed configuration in place (an extension, a trunk and a
// route, all projected into Asterisk; an inbound number, an emergency
// country and number, a call record), a platform administrator walks every
// page of the console, then adds a trunk with a secret and an extension
// (whose credentials are shown once) through the screens. Every API exchange and every rendered page is scanned:
// no credential travels or appears out of place, and no Asterisk
// identifier or vocabulary reaches the wire or the screen — except an
// extension's SIP username, in the one API field that carries it.

let rig = false;
test.beforeAll(async () => {
  rig = await rigAvailable();
});
test.beforeEach(() => {
  test.skip(!rig, "the dev rig is not running (make tilt-up)");
});

let user: RigUser | undefined;
// teardown removes, in order, what the test created through the API (a
// route before its trunk), then the seeded fixture.
let teardown: (() => Promise<void>) | undefined;
test.afterEach(async () => {
  await teardown?.();
  teardown = undefined;
  user?.cleanup();
  user = undefined;
});

const PAGES = ["/", "/licence", "/users", "/extensions", "/trunks", "/calls", "/account"];

async function call(request: APIRequestContext, token: string, procedure: string, data: unknown) {
  const res = await request.post(`/atsapbx.v1.${procedure}`, { data, headers: { Authorization: `Bearer ${token}` } });
  expect(res.status(), `${procedure}: ${await res.text()}`).toBe(200);
  return res.json() as Promise<Record<string, Record<string, string>>>;
}

test("the scanner recognises what it must reject", () => {
  // A scanner that matches nothing would pass every page; prove each
  // pattern fires on a realistic sample.
  for (const sample of [
    "e_0123456789abcdef0123456789abcdef",
    "PJSIP/e_0123456789abcdef0123456789abcdef-00000001",
    "1791844407.123",
    "bridge-4f9c2a1e-77aa",
    "ps_endpoints",
    "pjsip.conf",
    "stasis-in",
  ]) {
    expect(engineFindings("sample", sample), sample).not.toEqual([]);
  }
  expect(engineFindings("sample", '{"authUsername":"e_0123456789abcdef0123456789abcdef"}')).toEqual([]);
});

test("no credential or engine identifier crosses the wire or reaches the screen", async ({ page, request, context }) => {
  user = seedUser(["Acme"], { platformAdmin: true });
  const tenantId = user.tenants[0]!.id;

  // Engine-backed configuration, through the public API.
  const signIn = await request.post("/atsapbx.v1.AuthService/SignIn", { data: { email: user.email, password: FIXTURE_PASSWORD } });
  const token = ((await signIn.json()) as { access: { token: string } }).access.token;
  // Everything the test makes, through the API or the screens, is removed
  // through the API: numbers and routes before trunks and extensions.
  teardown = async () => {
    const list = async (procedure: string, key: string) => ((await call(request, token, procedure, { tenantId })) as Record<string, unknown>)[key] as Record<string, string>[] | undefined;
    for (const n of (await list("PbxService/ListInboundNumbers", "numbers")) ?? []) await call(request, token, "PbxService/DeleteInboundNumber", { tenantId, inboundNumberId: n.inboundNumberId });
    for (const r of (await list("PbxService/ListRoutes", "routes")) ?? []) await call(request, token, "PbxService/DeleteRoute", { tenantId, routeId: r.routeId });
    for (const t of (await list("PbxService/ListTrunks", "trunks")) ?? []) await call(request, token, "PbxService/DeleteTrunk", { tenantId, trunkId: t.trunkId });
    for (const e of (await list("PbxService/ListExtensions", "extensions")) ?? []) await call(request, token, "PbxService/DeleteExtension", { tenantId, extensionId: e.extensionId });
  };
  const ext = (await call(request, token, "PbxService/CreateExtension", {
    tenantId, number: `78${String(Date.now()).slice(-4)}`, displayName: "Exposure e2e", deviceType: "DEVICE_TYPE_SIP",
  })).extension!;
  const trunk = (await call(request, token, "PbxService/CreateTrunk", {
    tenantId, name: "Exposure carrier", host: "198.51.100.7", channelLimit: 4, priority: 1,
  })).trunk!;
  await call(request, token, "PbxService/CreateRoute", { tenantId, prefixPattern: "+237", trunkId: trunk.trunkId, costPerMinuteMicros: 1000, priority: 1 });
  await call(request, token, "PbxService/CreateInboundNumber", { tenantId, number: "+237222000777", extensionId: ext.extensionId });
  await call(request, token, "PbxService/SetEmergencyProfile", { tenantId, countryCode: "CM" });
  await call(request, token, "PbxService/CreateEmergencyNumber", { tenantId, number: "1500" });
  seedCalls(tenantId, [
    { direction: "OUTBOUND", state: "Terminated", from: "7801", to: "+237699000001", startedAt: new Date(Date.now() - 3_600_000).toISOString(), answeredAt: new Date(Date.now() - 3_590_000).toISOString(), endedAt: new Date(Date.now() - 3_530_000).toISOString(), reason: "normal clearing" },
  ]);

  // Now the person, in a real browser, with every exchange recorded.
  const exchanges = recordTraffic(page);
  await page.goto("/sign-in");
  await page.getByLabel("Email").fill(user.email);
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByLabel("Password", { exact: true }).fill(FIXTURE_PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
  await expect(page.locator("li.checklist-item", { hasText: "Add an extension" })).toContainText("Done");
  const refreshSecret = (await context.cookies()).find((c) => c.name === "atsa_refresh")?.value;
  expect(refreshSecret, "the session cookie exists").toBeTruthy();

  const bogusKey = "eyJleHBvc3VyZSI6InRlc3QifQ.ZXhwb3N1cmUta2V5.c2ln";
  const trunkSecret = `exposure-trunk-secret-${Date.now()}`;
  const creds = {
    password: FIXTURE_PASSWORD,
    licenceKey: bogusKey,
    refreshSecret: refreshSecret!,
    writeOnly: [{ name: "trunk secret", value: trunkSecret, sentTo: ["atsapbx.v1.PbxService/CreateTrunk"] }],
  };
  const findings: Finding[] = [];

  for (const path of PAGES) {
    await page.goto(path);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await page.waitForLoadState("networkidle");
    findings.push(...renderFindings(path, await renderedSurface(page), exchanges, creds));
  }

  // A licence key that does not verify: refused, and the key is neither
  // echoed by the server nor left on the page.
  await page.goto("/licence");
  await page.getByLabel("Licence key").fill(bogusKey);
  await page.getByRole("button", { name: "Apply licence" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Apply licence" }).click();
  await expect(page.getByLabel("Licence key")).toHaveAccessibleDescription(/did not verify/);
  findings.push(...renderFindings("/licence after a refused key", await renderedSurface(page), exchanges, creds));

  // A trunk with a secret, added through the screen: the secret goes out
  // once, in CreateTrunk, and never comes back or shows.
  await page.goto("/trunks");
  await page.getByRole("button", { name: "Add trunk" }).click();
  const trunkDialog = page.getByRole("dialog", { name: "Add a trunk" });
  await trunkDialog.getByLabel("Name", { exact: true }).fill("Secret carrier");
  await trunkDialog.getByLabel("Host").fill("198.51.100.8");
  await trunkDialog.getByLabel("Channel limit").fill("2");
  await trunkDialog.getByLabel("Username (optional)").fill("acct-exposure");
  await trunkDialog.getByLabel("Secret (optional)").fill(trunkSecret);
  await trunkDialog.getByRole("button", { name: "Add trunk" }).click();
  await expect(page.getByText("Trunk Secret carrier added.")).toBeVisible();
  findings.push(...renderFindings("/trunks after adding a trunk with a secret", await renderedSurface(page), exchanges, creds));

  // An extension added through the screen: its credentials are shown once,
  // the SIP username only in its marked element.
  await page.goto("/extensions");
  await page.getByRole("button", { name: "Add extension" }).click();
  const extDialog = page.getByRole("dialog", { name: "Add an extension" });
  const number = `79${String(Date.now()).slice(-4)}`;
  await extDialog.getByLabel("Number").fill(number);
  await extDialog.getByLabel("Name").fill("Exposure panel");
  await extDialog.getByRole("button", { name: "Add extension" }).click();
  const panel = page.getByRole("dialog", { name: `Extension ${number} created` });
  const sipUsername = (await panel.locator("[data-sip-username]").textContent())!.trim();
  const sipPassword = (await panel.locator(".secret-field", { hasText: "SIP password" }).locator(".secret-value").textContent())!.trim();
  expect(sipUsername).toMatch(/^e_[0-9a-f]{32}$/);
  findings.push(...renderFindings("credential panel", await renderedSurface(page), exchanges, creds));
  await panel.getByRole("checkbox", { name: "I have saved these details" }).check();
  await panel.getByRole("button", { name: "Done" }).click();
  // Once dismissed, the password is nowhere: not on any page, not in any
  // request, and in no response but the one that created it.
  for (const path of PAGES) {
    await page.goto(path);
    await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
    await page.waitForLoadState("networkidle");
    const surface = await renderedSurface(page);
    findings.push(...renderFindings(`${path} after the credential panel`, surface, exchanges, creds));
    findings.push(...secretFindings(`${path} after the credential panel`, surface, { "SIP password": sipPassword }));
  }
  for (const e of exchanges) {
    findings.push(...secretFindings(`${e.procedure} request`, e.requestBody + e.url, { "SIP password": sipPassword }));
    if (!e.procedure.endsWith("PbxService/CreateExtension")) findings.push(...secretFindings(`${e.procedure} response`, e.responseBody, { "SIP password": sipPassword }));
  }

  findings.push(...trafficFindings(exchanges, creds));
  expect(findings).toEqual([]);

  // The scan was not vacuous: the engine-backed data did cross the wire.
  const procedures = new Set(exchanges.map((e) => e.procedure));
  for (const p of [
    "PbxService/ListExtensions", "PbxService/ListTrunks", "PbxService/ListRoutes", "PbxService/ListInboundNumbers", "PbxService/GetEmergencyProfile",
    "TelephonyService/ListCalls", "PbxService/CreateTrunk", "PbxService/CreateExtension", "LicensingService/ApplyLicenseKey",
  ]) {
    expect(procedures, p).toContain(`atsapbx.v1.${p}`);
  }
  expect(exchanges.some((e) => e.responseBody.includes(ext.authUsername!)), "the SIP username was on the wire, in its field").toBe(true);
  expect(exchanges.find((e) => e.procedure.endsWith("ApplyLicenseKey"))?.requestBody).toContain(bogusKey);
  expect(exchanges.find((e) => e.procedure.endsWith("CreateTrunk"))?.requestBody, "the trunk secret was sent").toContain(trunkSecret);
  expect(exchanges.find((e) => e.procedure.endsWith("CreateExtension"))?.responseBody, "the SIP password came back once").toContain(sipPassword);
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
});
