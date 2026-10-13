import { expect, test } from "@playwright/test";
import { FIXTURE_PASSWORD, forgetUser, rigAvailable, seedUser, signIn, type RigUser } from "./support/rig";

// console-admin-screens: HLD-18 A2 (users, roles, permissions) against the
// real API on the dev rig.

let rig = false;
test.beforeAll(async () => {
  rig = await rigAvailable();
});
test.beforeEach(() => {
  test.skip(!rig, "the dev rig is not running (make tilt-up)");
});

let user: RigUser | undefined;
const created: string[] = [];
test.afterEach(() => {
  for (const email of created.splice(0)) forgetUser(email);
  user?.cleanup();
  user = undefined;
});

test("A2: the users list shows this tenant's real members and roles", async ({ page }) => {
  user = seedUser(["Acme"]);
  await signIn(page, user.email);
  await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Users", exact: true }).click();
  const table = page.getByRole("table", { name: "Users of this tenant" });
  const row = table.getByRole("row", { name: new RegExp(user.email) });
  await expect(row).toContainText("Tenant administrator");
  await expect(row).toContainText("Active");
  await expect(table.getByRole("row")).toHaveCount(2); // header + the one member
});

test("A2: an administrator adds a person, who can then sign in", async ({ page, browser }) => {
  user = seedUser(["Acme"]);
  const email = `console-e2e-added-${Date.now()}@example.test`;
  const password = "added person passphrase";
  created.push(email);

  await signIn(page, user.email);
  await page.goto("/users");
  await page.getByRole("button", { name: "Add user" }).click();
  const dialog = page.getByRole("dialog", { name: "Add a user" });
  await dialog.getByLabel("Email").fill(email);
  await dialog.getByLabel("Name").fill("Added Person");
  await dialog.getByLabel("Role").selectOption("SUPERVISOR");
  await dialog.getByLabel("Initial password (optional)").fill(password);
  await dialog.getByRole("button", { name: "Add user" }).click();
  await expect(page.getByText("Added Person added to this tenant.")).toBeVisible();
  const row = page.getByRole("table", { name: "Users of this tenant" }).getByRole("row", { name: new RegExp(email) });
  await expect(row).toContainText("Supervisor");
  expect(await page.content()).not.toContain(password);

  // The new person signs in with the initial password.
  const theirs = await browser.newContext();
  const them = await theirs.newPage();
  await signIn(them, email, password);
  await expect(them.locator(".tenant-indicator")).toContainText(user.tenants[0]!.name);
  await theirs.close();
});

test("A2: grant and revoke a role; the last administrator cannot be removed", async ({ page }) => {
  user = seedUser(["Acme"]);
  await signIn(page, user.email);
  await page.goto("/users");
  const actions = page.getByRole("button", { name: /^Actions for/ });

  // Grant Agent to the administrator, then revoke it again.
  await actions.click();
  await page.getByRole("menuitem", { name: "Grant a role…" }).click();
  await page.getByRole("dialog").getByLabel("Role").selectOption("AGENT");
  await page.getByRole("dialog").getByRole("button", { name: "Grant role" }).click();
  const row = page.getByRole("table", { name: "Users of this tenant" }).getByRole("row", { name: new RegExp(user.email) });
  await expect(row).toContainText("Agent");
  await actions.click();
  await page.getByRole("menuitem", { name: "Revoke Agent" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Revoke role" }).click();
  await expect(page.getByText("Agent revoked from")).toBeVisible();
  await expect(row).not.toContainText("Agent");

  // The server refuses to remove the tenant's last administrator.
  await actions.click();
  await page.getByRole("menuitem", { name: "Revoke Tenant administrator" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Revoke role" }).click();
  await expect(page.getByRole("dialog")).toContainText("is this tenant's last administrator. Make someone else a tenant administrator first.");
  await page.getByRole("dialog").getByRole("button", { name: "Cancel" }).click();
  await expect(row).toContainText("Tenant administrator");
});

test("A2: reset a password (shown once, must be changed) and disable a membership", async ({ page, browser, request }) => {
  user = seedUser(["Acme"]);
  const email = `console-e2e-reset-${Date.now()}@example.test`;
  created.push(email);
  // A second person, added through the public API.
  const adminToken = ((await (await request.post("/atsapbx.v1.AuthService/SignIn", { data: { email: user.email, password: FIXTURE_PASSWORD } })).json()) as { access: { token: string } }).access.token;
  const provisioned = await request.post("/atsapbx.v1.IdentityService/ProvisionPrincipal", {
    data: { tenantId: user.tenants[0]!.id, email, displayName: "Reset Person", role: "AGENT", password: "original passphrase 1" },
    headers: { Authorization: `Bearer ${adminToken}` },
  });
  expect(provisioned.status()).toBe(200);

  await signIn(page, user.email);
  await page.goto("/users");
  await page.getByRole("button", { name: "Actions for Reset Person" }).click();
  await page.getByRole("menuitem", { name: "Reset password…" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Reset password" }).click();
  const panel = page.getByRole("dialog", { name: "Temporary password for Reset Person" });
  const temporary = (await panel.locator(".secret-value").textContent())!.trim();
  expect(temporary).toMatch(/^[A-HJ-NP-Za-km-z2-9]{16}$/);
  await panel.getByRole("checkbox", { name: "I have saved these details" }).check();
  await panel.getByRole("button", { name: "Done" }).click();
  expect(await page.content()).not.toContain(temporary);

  // The person signs in with the temporary password and must change it.
  const theirs = await browser.newContext();
  const them = await theirs.newPage();
  await them.goto("/sign-in");
  await them.getByLabel("Email").fill(email);
  await them.getByRole("button", { name: "Continue" }).click();
  await them.getByLabel("Password", { exact: true }).fill(temporary);
  await them.getByRole("button", { name: "Sign in" }).click();
  await expect(them.getByRole("heading", { level: 2, name: "Choose your own password" })).toBeVisible();
  await theirs.close();

  // Disable the membership: the person can no longer get in.
  await page.getByRole("button", { name: "Actions for Reset Person" }).click();
  await page.getByRole("menuitem", { name: "Disable…" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Disable" }).click();
  await expect(page.getByRole("table", { name: "Users of this tenant" }).getByRole("row", { name: /Reset Person/ })).toContainText("Disabled");
  const again = await browser.newContext();
  const blocked = await again.newPage();
  await blocked.goto("/sign-in");
  await blocked.getByLabel("Email").fill(email);
  await blocked.getByRole("button", { name: "Continue" }).click();
  await blocked.getByLabel("Password", { exact: true }).fill(temporary);
  await blocked.getByRole("button", { name: "Sign in" }).click();
  await expect(blocked.getByRole("heading", { level: 1, name: "Overview" })).toHaveCount(0);
  await expect(blocked.getByText(/No active access|The email or password is not correct/)).toBeVisible();
  await again.close();
});
