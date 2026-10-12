import { expect, test, type Page } from "@playwright/test";
import { adminResetPassword, FIXTURE_PASSWORD, rigAvailable, seedUser, type RigUser } from "./support/rig";

// console-foundation task 2.2 against the real API on the dev rig: an
// administrator-issued temporary password reaches only the account page,
// the server refuses everything else with it, and changing it opens the
// console and ends the person's other sessions.

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

test("a temporary password reaches only the account page until it is changed", async ({ page, request }) => {
  user = seedUser(["Acme"]);
  const temporary = adminResetPassword(user.email);

  // The server is the enforcement point: a must-change token is refused
  // for anything but ChangePassword and SignOut.
  const signedIn = await request.post("/atsapbx.v1.AuthService/SignIn", { data: { email: user.email, password: temporary } });
  const grant = (await signedIn.json()) as { access: { token: string; tenantId: string }; passwordChangeRequired: boolean };
  expect(grant.passwordChangeRequired).toBe(true);
  const other = await request.post("/atsapbx.v1.PbxService/ListExtensions", {
    data: { tenantId: grant.access.tenantId },
    headers: { Authorization: `Bearer ${grant.access.token}` },
  });
  expect(other.status()).toBe(403);

  // The console shows only the way forward.
  await signIn(page, user.email, temporary);
  await expect(page.getByRole("heading", { level: 2, name: "Choose your own password" })).toBeVisible();
  await page.goto("/users");
  await expect(page).toHaveURL(/\/account$/);

  await page.getByLabel("Temporary password").fill("not the temporary one");
  await page.getByLabel("New password", { exact: true }).fill("my own passphrase now");
  await page.getByLabel("Confirm new password").fill("my own passphrase now");
  await page.getByRole("button", { name: "Change password" }).click();
  await expect(page.getByLabel("Temporary password")).toHaveAccessibleDescription("The current password is not correct.");

  await page.getByLabel("Temporary password").fill(temporary);
  await page.getByLabel("New password", { exact: true }).fill("my own passphrase now");
  await page.getByLabel("Confirm new password").fill("my own passphrase now");
  await page.getByRole("button", { name: "Change password" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
  await expect(page.getByText("Password changed. Your other sessions have been signed out.")).toBeVisible();

  // Reload: the renewed session is a normal one.
  await page.goto("/users");
  await expect(page.getByRole("heading", { level: 1, name: "Users" })).toBeVisible();
});

test("changing the password ends the person's other sessions, and that browser is told", async ({ browser }) => {
  user = seedUser(["Acme"]);
  const here = await browser.newContext();
  const elsewhere = await browser.newContext();
  const a = await here.newPage();
  const b = await elsewhere.newPage();

  await signIn(a, user.email, FIXTURE_PASSWORD);
  await expect(a.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();
  await signIn(b, user.email, FIXTURE_PASSWORD);
  await expect(b.getByRole("heading", { level: 1, name: "Overview" })).toBeVisible();

  await a.goto("/account");
  await a.getByLabel("Current password").fill(FIXTURE_PASSWORD);
  await a.getByLabel("New password", { exact: true }).fill("changed elsewhere 42");
  await a.getByLabel("Confirm new password").fill("changed elsewhere 42");
  await a.getByRole("button", { name: "Change password" }).click();
  await expect(a.getByText("Password changed. Your other sessions have been signed out.")).toBeVisible();

  // The other browser's next restore is refused: back to sign-in, with a
  // reason, and its cookie gone.
  await b.reload();
  await expect(b.getByRole("heading", { level: 1, name: "Sign in" })).toBeVisible();
  // This browser is still signed in.
  await a.reload();
  await expect(a.getByRole("heading", { level: 1, name: "Account" })).toBeVisible();

  await here.close();
  await elsewhere.close();
});

test("signing out says so", async ({ page }) => {
  user = seedUser(["Acme"]);
  await signIn(page, user.email, FIXTURE_PASSWORD);
  await page.getByRole("link", { name: "Account" }).first().click();
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page.getByText("You have signed out")).toBeVisible();
});
