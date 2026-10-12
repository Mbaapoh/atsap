import { expect, test, type Page } from "@playwright/test";
import { FIXTURE_PASSWORD, latestLink, rigAvailable, seedUser, type RigUser } from "./support/rig";

// console-foundation task 2.1 against the real API on the dev rig (make
// tilt-up): sign-in, Tenant choice, restore through the HttpOnly cookie,
// sign-out, and recovery through an emailed link — with no credential
// ever reaching browser storage or the address bar.

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

async function signIn(page: Page, email: string, password: string) {
  await page.goto("/sign-in");
  await page.getByLabel("Email").fill(email);
  await page.getByRole("button", { name: "Continue" }).click();
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
}

// nothingStored asserts what the page can reach holds no credential: both
// storages empty, the only cookie the server's HttpOnly, SameSite=Strict
// refresh cookie scoped to AuthService, and no secret in the address.
async function nothingStored(page: Page, ...secrets: string[]) {
  const storage = await page.evaluate(() => ({ local: localStorage.length, session: sessionStorage.length, cookie: document.cookie }));
  expect(storage).toEqual({ local: 0, session: 0, cookie: "" });
  const cookies = await page.context().cookies();
  for (const c of cookies) {
    expect(c.name).toBe("atsa_refresh");
    expect(c.httpOnly).toBe(true);
    expect(c.sameSite).toBe("Strict");
    expect(c.path).toBe("/atsapbx.v1.AuthService/");
  }
  for (const s of secrets) expect(page.url()).not.toContain(s);
}

test("sign in, choose a Tenant, survive a reload, switch, and sign out", async ({ page }) => {
  user = seedUser(["Acme", "Globex"]);
  const [acme, globex] = user.tenants;

  await signIn(page, user.email, FIXTURE_PASSWORD);
  await expect(page.getByRole("heading", { level: 1, name: "Choose a tenant" })).toBeVisible();
  await page.getByRole("button", { name: new RegExp(acme!.name) }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
  await expect(page.locator(".tenant-indicator")).toContainText(acme!.name);
  expect((await page.context().cookies()).map((c) => c.name)).toEqual(["atsa_refresh"]);
  await nothingStored(page, FIXTURE_PASSWORD);

  // A reload drops the in-memory token; the cookie restores the session,
  // in the Tenant chosen before.
  await page.goto("/users");
  await page.reload();
  await expect(page.getByRole("heading", { level: 1, name: "Users" })).toBeVisible();
  await expect(page.locator(".tenant-indicator")).toContainText(acme!.name);

  await page.getByRole("link", { name: "Switch tenant" }).click();
  await page.getByRole("button", { name: new RegExp(globex!.name) }).click();
  await expect(page.locator(".tenant-indicator")).toContainText(globex!.name);

  await page.getByRole("link", { name: "Account" }).first().click();
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Sign in" })).toBeVisible();
  expect(await page.context().cookies()).toEqual([]);

  // Signed out means signed out: a reload does not bring it back.
  await page.goto("/users");
  await expect(page.getByRole("heading", { level: 1, name: "Sign in" })).toBeVisible();
});

test("a wrong password gets the same answer as an unknown email", async ({ page }) => {
  user = seedUser(["Acme"]);
  await signIn(page, user.email, "not-the-password");
  const known = await page.getByRole("alert").textContent();
  await signIn(page, `nobody-${Date.now()}@example.test`, "not-the-password");
  const unknown = await page.getByRole("alert").textContent();
  expect(known).toBe("The email or password is not correct.");
  expect(unknown).toBe(known);
});

test("recover a password through the emailed link", async ({ page }) => {
  user = seedUser(["Acme"]);
  const next = `recovered ${Date.now()}`;

  await page.goto("/forgot-password");
  await page.getByLabel("Email").fill(user.email);
  await page.getByRole("button", { name: "Send reset link" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Check your email" })).toBeVisible();

  const link = await latestLink(user.email);
  const secret = link.split("#")[1]!;
  await page.goto(new URL(link).pathname + "#" + secret);
  await expect(page.getByRole("heading", { level: 1, name: "Choose a new password" })).toBeVisible();
  await expect.poll(() => new URL(page.url()).hash).toBe("");
  await page.getByLabel("New password", { exact: true }).fill(next);
  await page.getByLabel("Confirm new password").fill(next);
  await page.getByRole("button", { name: "Set password" }).click();
  await expect(page.getByText("Your password is set")).toBeVisible();
  await nothingStored(page, secret, next);

  // The link works once. (Leave the page first: a fragment-only change of
  // the same address does not reload it.)
  await page.goto("/sign-in");
  await page.goto(new URL(link).pathname + "#" + secret);
  await page.getByLabel("New password", { exact: true }).fill(next);
  await page.getByLabel("Confirm new password").fill(next);
  await page.getByRole("button", { name: "Set password" }).click();
  await expect(page.getByRole("alert")).toContainText("This link has expired or was already used");

  // The old password is gone; the new one signs in (one Tenant: no picker).
  await signIn(page, user.email, FIXTURE_PASSWORD);
  await expect(page.getByRole("alert")).toHaveText("The email or password is not correct.");
  await signIn(page, user.email, next);
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
});
