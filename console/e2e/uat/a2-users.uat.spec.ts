import { expect, test } from "@playwright/test";
import { forgetUser, rigAvailable, seedUser, signIn, type RigUser } from "../support/rig";

// UAT — HLD-18 Flow A2, "Add users and roles", on the dev rig as people do
// it: the tenant administrator adds a supervisor with an initial password;
// the supervisor signs in and can see the tenant's people but change
// nothing — and the server, not just the screen, refuses; the
// administrator makes them an administrator too, then disables them, and
// they can no longer get in.

let rig = false;
test.beforeAll(async () => {
  rig = await rigAvailable();
});
test.beforeEach(() => {
  test.skip(!rig, "the dev rig is not running (make tilt-up)");
});

let admin: RigUser | undefined;
const created: string[] = [];
test.afterEach(() => {
  admin?.cleanup();
  admin = undefined;
  for (const email of created.splice(0)) forgetUser(email);
});

test("A2: add a supervisor who signs in read-only, promote them, then disable them", async ({ page, browser, request }) => {
  admin = seedUser(["People tenant"]);
  const email = `console-uat-sam-${Date.now()}@example.test`;
  const password = "sam initial passphrase";
  created.push(email);

  // The administrator adds Sam as a supervisor.
  await signIn(page, admin.email);
  await page.getByRole("navigation").getByRole("link", { name: "Users" }).click();
  await page.getByRole("button", { name: "Add user" }).click();
  const add = page.getByRole("dialog", { name: "Add a user" });
  await add.getByLabel("Email").fill(email);
  await add.getByLabel("Name").fill("Sam Supervisor");
  await add.getByLabel("Role").selectOption("SUPERVISOR");
  await add.getByLabel("Initial password (optional)").fill(password);
  await add.getByRole("button", { name: "Add user" }).click();
  const users = page.getByRole("table", { name: "Users of this tenant" });
  await expect(users.getByRole("row", { name: /Sam Supervisor/ })).toContainText("Supervisor");

  // Sam signs in: sees the people, can change nothing.
  const samContext = await browser.newContext();
  const sam = await samContext.newPage();
  await signIn(sam, email, password);
  await sam.getByRole("navigation").getByRole("link", { name: "Users" }).click();
  await expect(sam.getByRole("table", { name: "Users of this tenant" }).getByRole("row", { name: new RegExp(admin.email) })).toBeVisible();
  await expect(sam.getByRole("button", { name: "Add user" })).toHaveCount(0);
  await expect(sam.getByRole("button", { name: /^Actions for/ })).toHaveCount(0);
  // Hiding a control is not enforcement: the server refuses Sam too.
  const samToken = ((await (await request.post("/atsapbx.v1.AuthService/SignIn", { data: { email, password } })).json()) as { access: { token: string } }).access.token;
  const refused = await request.post("/atsapbx.v1.IdentityService/ProvisionPrincipal", {
    data: { tenantId: admin.tenants[0]!.id, email: `never-${Date.now()}@example.test`, displayName: "Never", role: "AGENT" },
    headers: { Authorization: `Bearer ${samToken}` },
  });
  expect(refused.status()).toBe(403);
  await samContext.close();

  // The administrator makes Sam a tenant administrator as well.
  await page.getByRole("button", { name: "Actions for Sam Supervisor" }).click();
  await page.getByRole("menuitem", { name: "Grant a role…" }).click();
  await page.getByRole("dialog").getByLabel("Role").selectOption("TENANT_ADMIN");
  await page.getByRole("dialog").getByRole("button", { name: "Grant role" }).click();
  await expect(users.getByRole("row", { name: /Sam Supervisor/ })).toContainText("Tenant administrator");

  // Then disables Sam in this tenant: Sam can no longer get in.
  await page.getByRole("button", { name: "Actions for Sam Supervisor" }).click();
  await page.getByRole("menuitem", { name: "Disable…" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Disable" }).click();
  await expect(users.getByRole("row", { name: /Sam Supervisor/ })).toContainText("Disabled");
  const laterContext = await browser.newContext();
  const later = await laterContext.newPage();
  await later.goto("/sign-in");
  await later.getByLabel("Email").fill(email);
  await later.getByRole("button", { name: "Continue" }).click();
  await later.getByLabel("Password", { exact: true }).fill(password);
  await later.getByRole("button", { name: "Sign in" }).click();
  await expect(later.getByText(/No active access|The email or password is not correct/)).toBeVisible();
  await expect(later.getByRole("heading", { level: 1, name: "Overview" })).toHaveCount(0);
  await laterContext.close();
});
