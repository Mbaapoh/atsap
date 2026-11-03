import { expect, test, type APIRequestContext, type Page } from "@playwright/test";
import { FIXTURE_PASSWORD, rigAvailable, seedUser, signIn, type RigUser } from "./support/rig";

// console-browser-phone: two real browsers register WEBRTC extensions over
// the rig's WebSocket transport and call each other. Audio is not measured
// here (UAT's job); the SIP sessions, the console states and the call
// record are the proof.

test.use({
  permissions: ["microphone"],
  launchOptions: { args: ["--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream"] },
});

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

async function apiToken(request: APIRequestContext, email: string): Promise<string> {
  const res = await request.post("/atsapbx.v1.AuthService/SignIn", { data: { email, password: FIXTURE_PASSWORD } });
  return ((await res.json()) as { access: { token: string } }).access.token;
}

interface WebRTC {
  id: string;
  number: string;
  name: string;
  secret: string;
}

async function createWebRTC(request: APIRequestContext, token: string, tenantId: string, number: string, name: string): Promise<WebRTC> {
  const res = await request.post("/atsapbx.v1.PbxService/CreateExtension", {
    data: { tenantId, number, displayName: name, deviceType: "DEVICE_TYPE_WEBRTC" },
    headers: { Authorization: `Bearer ${token}` },
  });
  expect(res.status()).toBe(200);
  const body = (await res.json()) as { extension: { extensionId: string; number: string; displayName: string }; secret: string };
  return { id: body.extension.extensionId, number: body.extension.number, name: body.extension.displayName, secret: body.secret };
}

async function connect(page: Page, email: string, ext: WebRTC) {
  await signIn(page, email);
  await page.goto("/phone");
  const select = page.getByLabel("WEBRTC extension");
  await expect(select.locator("option", { hasText: ext.number })).toHaveCount(1);
  await select.selectOption({ label: `${ext.number} · ${ext.name}` });
  await page.getByLabel("SIP password").fill(ext.secret);
  await page.getByRole("button", { name: "Connect" }).click();
  await expect(page.getByText(`Registered as ${ext.number}`)).toBeVisible({ timeout: 15_000 });
}

test("browser phone: two WEBRTC extensions register, call, answer and hang up", async ({ page, browser, request }) => {
  test.setTimeout(90_000);
  user = seedUser(["Acme"]);
  const tenantId = user.tenants[0]!.id;
  const token = await apiToken(request, user.email);
  const stamp = String(Date.now()).slice(-4);
  const a = await createWebRTC(request, token, tenantId, `81${stamp}`, "Browser A");
  const b = await createWebRTC(request, token, tenantId, `82${stamp}`, "Browser B");
  teardown = async () => {
    for (const id of [a.id, b.id]) {
      await request.post("/atsapbx.v1/PbxService/DeleteExtension", { data: { tenantId, extensionId: id }, headers: { Authorization: `Bearer ${token}` } });
    }
  };

  const contextB = await browser.newContext({ permissions: ["microphone"] });
  const pageB = await contextB.newPage();
  try {
    await connect(page, user.email, a);
    await connect(pageB, user.email, b);

    await page.getByLabel("Number to call").fill(b.number);
    await page.getByRole("button", { name: "Call", exact: true }).click();

    await expect(pageB.getByText(`Incoming call from ${a.number} · Browser A`)).toBeVisible({ timeout: 15_000 });
    await pageB.getByRole("button", { name: "Answer" }).click();

    await expect(page.getByText(`In call with ${b.number} · Browser B`)).toBeVisible({ timeout: 15_000 });
    await expect(pageB.getByText(`In call with ${a.number} · Browser A`)).toBeVisible({ timeout: 15_000 });

    await page.getByRole("button", { name: "Hang up" }).click();
    await expect(page.getByText("Call ended")).toBeVisible({ timeout: 10_000 });

    // The call is an ordinary extension call: the platform records it.
    await expect
      .poll(async () => {
        const res = await request.post("/atsapbx.v1.TelephonyService/ListCalls", {
          data: { tenantId, limit: 10 },
          headers: { Authorization: `Bearer ${token}` },
        });
        const calls = ((await res.json()) as { calls?: { direction: string; sourceNumber: string; destNumber: string }[] }).calls ?? [];
        return calls.some((c) => c.direction === "INTERNAL" && c.sourceNumber === a.number && c.destNumber === b.number);
      }, { timeout: 10_000 })
      .toBe(true);
  } finally {
    await contextB.close();
  }
});
