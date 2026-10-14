import { expect, test, type APIRequestContext } from "@playwright/test";
import { engineFindings, renderedSurface } from "../support/exposure";
import { FIXTURE_PASSWORD, rigAvailable, seedUser, signIn, type RigUser } from "../support/rig";
import { baresipInstalled, engineReachesHostAt, Phone, portsFree } from "./support/phone";

// UAT — HLD-18 Flows A3, A4 and A5 on the dev rig, the way a tenant
// administrator does them, with real phones:
//   A3  create an extension in the console and configure a real SIP phone
//       with exactly what the console shows once; the console then shows
//       it registered;
//   A4  connect a carrier and an outbound route, point a public number at
//       the extension and set the emergency country, in the console;
//   A5  a real call between the phone and the carrier (placed through the
//       public API: the console has no dialler in Phase A) appears in the
//       console's call history with its actual state, duration and reason.
// The carrier is a second baresip, IP-authenticated and answering any
// number, as in scripts/uat-placement.sh.

const PHONE = { sipPort: 5070, ctrlPort: 5550, rtpPorts: "18000-18019" };
const CARRIER = { sipPort: 5071, ctrlPort: 5551, rtpPorts: "18020-18039" };
const DIALLED = "+442071234567";

let rig = false;
test.beforeAll(async () => {
  rig = await rigAvailable();
});
test.beforeEach(() => {
  test.skip(!rig, "the dev rig is not running (make tilt-up)");
  expect(baresipInstalled(), "baresip must be installed for the phone UAT").toBe(true);
  expect(portsFree(PHONE.sipPort, CARRIER.sipPort), "UDP 5070/5071 must be free (stop other baresip UATs)").toBe(true);
});

let admin: RigUser | undefined;
const phones: Phone[] = [];
let teardown: (() => Promise<void>) | undefined;
test.afterEach(async () => {
  for (const p of phones.splice(0)) p.stop();
  await teardown?.();
  teardown = undefined;
  admin?.cleanup();
  admin = undefined;
});

async function rpc(request: APIRequestContext, token: string, procedure: string, data: unknown) {
  const res = await request.post(`/atsapbx.v1.${procedure}`, { data, headers: { Authorization: `Bearer ${token}` } });
  return { status: res.status(), body: (await res.json().catch(() => ({}))) as Record<string, any> };
}

test("A3–A5: a phone registers with the console's credentials, and its real call appears in the history", async ({ page, request }) => {
  test.setTimeout(150_000);
  admin = seedUser(["Phone tenant"]);
  const tenantId = admin.tenants[0]!.id;
  const token = ((await (await request.post("/atsapbx.v1.AuthService/SignIn", { data: { email: admin.email, password: FIXTURE_PASSWORD } })).json()) as { access: { token: string } }).access.token;
  teardown = async () => {
    for (const r of (await rpc(request, token, "PbxService/ListRoutes", { tenantId })).body.routes ?? []) await rpc(request, token, "PbxService/DeleteRoute", { tenantId, routeId: r.routeId });
    for (const t of (await rpc(request, token, "PbxService/ListTrunks", { tenantId })).body.trunks ?? []) await rpc(request, token, "PbxService/DeleteTrunk", { tenantId, trunkId: t.trunkId });
    for (const n of (await rpc(request, token, "PbxService/ListInboundNumbers", { tenantId })).body.numbers ?? []) await rpc(request, token, "PbxService/DeleteInboundNumber", { tenantId, inboundNumberId: n.inboundNumberId });
    for (const e of (await rpc(request, token, "PbxService/ListExtensions", { tenantId })).body.extensions ?? []) await rpc(request, token, "PbxService/DeleteExtension", { tenantId, extensionId: e.extensionId });
  };
  const number = `88${String(Date.now()).slice(-4)}`;

  // --- A3: create the extension; the credentials are shown once --------
  await signIn(page, admin.email);
  await page.getByRole("navigation").getByRole("link", { name: "Extensions" }).click();
  await page.getByRole("button", { name: "Add extension" }).click();
  const add = page.getByRole("dialog", { name: "Add an extension" });
  await add.getByLabel("Number").fill(number);
  await add.getByLabel("Name").fill("Front desk");
  await add.getByRole("button", { name: "Add extension" }).click();

  const panel = page.getByRole("dialog", { name: `Extension ${number} created` });
  const shown = async (label: string) =>
    (await panel.locator(".secret-field").filter({ has: page.locator("dt", { hasText: new RegExp(`^${label}$`) }) }).locator(".secret-value").textContent())!.trim();
  const server = { host: await shown("SIP server"), port: await shown("Port"), transport: (await shown("Transport")).toLowerCase() };
  const username = await shown("SIP username");
  const password = await shown("SIP password");
  expect(engineFindings("credential panel", await renderedSurface(page))).toEqual([]);
  await panel.getByRole("checkbox", { name: "I have saved these details" }).check();
  await panel.getByRole("button", { name: "Done" }).click();

  // Configure a real phone with exactly what the console showed.
  const phone = new Phone({
    ...PHONE,
    account: `<sip:${username}@${server.host}:${server.port};transport=${server.transport}>;auth_user=${username};auth_pass=${password};outbound="sip:${server.host}:${server.port};transport=${server.transport}";regint=300`,
  });
  phones.push(phone);
  const row = page.getByRole("table", { name: "Extensions of this tenant" }).getByRole("row", { name: new RegExp(number) });
  await expect(row).toContainText("Registered", { timeout: 45_000 }); // the list re-reads registration every 15 s
  await expect(row).not.toContainText("Not registered");

  // --- A4: a carrier and a route to it --------------------------------------
  const carrierHost = engineReachesHostAt();
  // The carrier is up before its trunk is added, as a real one is: the
  // platform checks a new trunk at once (pbx-trunk-health-probing).
  const carrier = new Phone({ ...CARRIER, account: `<sip:carrier@${carrierHost}:${CARRIER.sipPort};transport=udp>;regint=0;catchall=yes` });
  phones.push(carrier);
  await page.getByRole("navigation").getByRole("link", { name: "Trunks & routes" }).click();
  await page.getByRole("button", { name: "Add trunk" }).click();
  const trunk = page.getByRole("dialog", { name: "Add a trunk" });
  await trunk.getByLabel("Name", { exact: true }).fill("UAT carrier");
  await trunk.getByLabel("Host").fill(carrierHost);
  await trunk.getByLabel("Port (optional)").fill(String(CARRIER.sipPort));
  await trunk.getByLabel("Channel limit").fill("2");
  await trunk.getByRole("button", { name: "Add trunk" }).click();
  await expect(page.getByText("Trunk UAT carrier added.")).toBeVisible();
  // Its health is the real carrier answering the platform's check, shown
  // within seconds without a reload.
  await expect(page.getByRole("table", { name: "Trunks of this tenant" }).getByRole("row", { name: /UAT carrier/ })).toContainText(/Healthy\s*Answering · checked /, { timeout: 20_000 });
  await page.getByRole("button", { name: "Add route" }).click();
  const route = page.getByRole("dialog", { name: "Add an outbound route" });
  await route.getByLabel("Prefix").fill("+44");
  await route.getByRole("button", { name: "Add route" }).click();
  await expect(page.getByRole("table", { name: "Outbound routes of this tenant" }).getByRole("row", { name: /\+44 UAT carrier/ })).toBeVisible();

  // The public number the carrier delivers rings the front desk, and the
  // tenant's emergency country is set.
  await page.getByRole("button", { name: "Assign number" }).click();
  const assign = page.getByRole("dialog", { name: "Assign an inbound number" });
  await assign.getByLabel("Number").fill("+237222000500");
  await assign.getByLabel("Rings extension").selectOption({ label: `${number} · Front desk` });
  await assign.getByRole("button", { name: "Assign number" }).click();
  await expect(page.getByText(`+237222000500 now rings ${number} · Front desk.`)).toBeVisible();
  const emergency = page.getByRole("region", { name: "Emergency numbers", exact: true });
  await expect(emergency.getByText("No emergency country is set")).toBeVisible();
  await emergency.getByRole("button", { name: "Set country" }).click();
  await page.getByRole("dialog").getByLabel("Country code").fill("CM");
  await page.getByRole("dialog").getByRole("button", { name: "Save country" }).click();
  await expect(emergency).toContainText("Country: CM.");
  await expect(emergency.getByRole("row", { name: /^117 Built in/ })).toBeVisible();

  await phone.autoAnswer();
  await carrier.autoAnswer();

  // --- A5: a real call ------------------------------------------------------
  const extensionId = ((await rpc(request, token, "PbxService/ListExtensions", { tenantId })).body.extensions as { extensionId: string; number: string }[]).find((e) => e.number === number)!.extensionId;
  const placed = await rpc(request, token, "PbxService/PlaceCall", { tenantId, extensionId, destination: DIALLED });
  expect(placed, JSON.stringify(placed.body)).toMatchObject({ status: 200 });
  const callId = placed.body.callId as string;
  const stateOf = async () => (await rpc(request, token, "TelephonyService/GetCall", { tenantId, callId })).body.state as string;
  await expect.poll(stateOf, { timeout: 20_000 }).toBe("Active");
  await page.waitForTimeout(3_000); // let it be answered for a few seconds
  expect((await rpc(request, token, "PbxService/HangupCall", { tenantId, callId })).status).toBe(200);
  await expect.poll(stateOf, { timeout: 15_000 }).toBe("Terminated");

  await page.getByRole("navigation").getByRole("link", { name: "Call history" }).click();
  const form = page.getByRole("form", { name: "Call filters" });
  await form.getByLabel("Direction").selectOption("OUTBOUND");
  await form.getByLabel("State").selectOption("Terminated");
  await form.getByRole("button", { name: "Apply filters" }).click();
  const call = page.getByRole("table", { name: "Calls of this tenant" }).getByRole("row", { name: new RegExp(DIALLED.replace("+", "\\+")) });
  await expect(call).toBeVisible();
  await expect(call).toContainText("Outbound");
  await expect(call).toContainText("Terminated");
  const cells = await call.getByRole("cell").allTextContents();
  expect(cells[5], "an answered call has a duration").toMatch(/^\d+:\d{2}$/);
  expect(cells[6], "the termination reason is the server's, verbatim").not.toBe("—");
  expect(engineFindings("call history", await renderedSurface(page))).toEqual([]);
  expect(await page.locator("main").textContent()).not.toMatch(/\bAnswered\b|\bMissed\b|\bFailed\b/);
});
