import { expect, test, type Page } from "@playwright/test";
import { FIXTURE_PASSWORD, rigAvailable, seedUser, type RigUser } from "./support/rig";

// console-foundation task 2.3 against the real API on the dev rig: the
// licence page shows what the server reports, a tenant administrator is
// offered no way to apply a key and the server refuses one anyway, and a
// key that does not verify changes nothing.

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

async function signIn(page: Page, email: string) {
  await page.goto("/sign-in");
  await page.getByLabel("Email").fill(email);
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByLabel("Password", { exact: true }).fill(FIXTURE_PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
}

test("a tenant administrator sees the server's licence, and cannot apply one", async ({ page, request }) => {
  user = seedUser(["Acme"]);
  await signIn(page, user.email);

  // The rig runs on a 16-channel development licence (make dev-licence).
  await expect(page.getByRole("link", { name: /^Licence: Valid · 16 channels/ })).toBeVisible();
  await page.getByRole("link", { name: /^Licence:/ }).click();
  await expect(page.locator(".licence-condition")).toHaveText("Valid");
  const facts = page.locator("dl.facts");
  await expect(facts).toContainText("Commercial");
  await expect(facts).toContainText("development");
  await expect(facts).toContainText(/\d+ in use of 16/);
  await expect(page.getByLabel("Licence key")).toHaveCount(0);

  // Hiding is not enforcement: the server refuses the call itself.
  const signedIn = await request.post("/atsapbx.v1.AuthService/SignIn", { data: { email: user.email, password: FIXTURE_PASSWORD } });
  const token = ((await signedIn.json()) as { access: { token: string } }).access.token;
  const apply = await request.post("/atsapbx.v1.LicensingService/ApplyLicenseKey", {
    data: { token: "anything" },
    headers: { Authorization: `Bearer ${token}` },
  });
  expect(apply.status()).toBe(403);
});

test("a key that does not verify is refused and the licence in force is untouched", async ({ page }) => {
  user = seedUser(["Acme"], { platformAdmin: true });
  await signIn(page, user.email);
  await page.goto("/licence");
  await expect(page.locator(".licence-condition")).toHaveText("Valid");

  const bogus = "eyJub3QiOiJhIGxpY2VuY2UifQ.Ym9ndXM.Ym9ndXM";
  await page.getByLabel("Licence key").fill(bogus);
  await page.getByRole("button", { name: "Apply licence" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Apply licence" }).click();

  await expect(page.getByLabel("Licence key")).toHaveAccessibleDescription(/This key did not verify/);
  await expect(page.locator(".licence-condition")).toHaveText("Valid");
  expect(await page.content()).not.toContain(bogus);
});
