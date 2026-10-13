import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { routes } from "../../app/routes";
import { PAGE_SIZE } from "../../components/Pager";
import { OVERVIEW_URLS, server } from "../../test/msw";
import { renderApp, tenantA } from "../../test/session";

// console-admin-screens task 2.1: the users list (HLD-18 A2).

export const ana = {
  principal: { principalId: "p-ana", email: "ana@example.com", displayName: "Ana Mbe", status: "PRINCIPAL_STATUS_ACTIVE" },
  roleBindings: [{ role: "TENANT_ADMIN", scope: "tenant" }],
};
export const ben = {
  principal: { principalId: "p-ben", email: "ben@example.com", displayName: "Ben Tabi", status: "PRINCIPAL_STATUS_DISABLED" },
  roleBindings: [{ role: "SUPERVISOR", scope: "tenant" }, { role: "BILLING_CLERK", scope: "tenant" }],
};

describe("users list", () => {
  it("lists this tenant's members with status and roles, for the session's tenant", async () => {
    const bodies: unknown[] = [];
    server.use(
      http.post(OVERVIEW_URLS.principals, async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ principals: [ana, ben] });
      }),
    );
    renderApp(routes, "/users");

    const table = within(await screen.findByRole("table", { name: "Users of this tenant" }));
    expect(table.getByText("Ana Mbe")).toBeInTheDocument();
    expect(table.getByText("ana@example.com")).toBeInTheDocument();
    expect(table.getByText("Tenant administrator")).toBeInTheDocument();
    expect(table.getByText("Active")).toBeInTheDocument();
    expect(table.getByText("Disabled")).toBeInTheDocument();
    // An unknown role is shown by its server name, never hidden.
    expect(table.getByText("Supervisor, BILLING_CLERK")).toBeInTheDocument();
    expect(bodies[0]).toEqual({ tenantId: tenantA.tenantId, limit: PAGE_SIZE });
    // Phase A: no extension column (D-65).
    expect(table.queryByRole("columnheader", { name: /extension/i })).not.toBeInTheDocument();
  });

  it("pages forward only after a full page", async () => {
    const offsets: number[] = [];
    server.use(
      http.post(OVERVIEW_URLS.principals, async ({ request }) => {
        const body = (await request.json()) as { offset?: number };
        offsets.push(body.offset ?? 0);
        const count = (body.offset ?? 0) === 0 ? PAGE_SIZE : 2;
        return HttpResponse.json({
          principals: Array.from({ length: count }, (_, i) => ({ ...ana, principal: { ...ana.principal, principalId: `p${body.offset ?? 0}-${i}` } })),
        });
      }),
    );
    const user = userEvent.setup();
    renderApp(routes, "/users");
    await user.click(await screen.findByRole("button", { name: "Next" }));
    expect(await screen.findByText("Page 2")).toBeInTheDocument();
    expect(offsets).toEqual([0, PAGE_SIZE]);
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
  });

  it("an empty tenant says so", async () => {
    server.use(http.post(OVERVIEW_URLS.principals, () => HttpResponse.json({})));
    renderApp(routes, "/users");
    expect(await screen.findByText("No users yet")).toBeInTheDocument();
  });

  it("a role that may not read users is told so plainly", async () => {
    server.use(http.post(OVERVIEW_URLS.principals, () => HttpResponse.json({ code: "permission_denied", message: "not permitted" }, { status: 403 })));
    renderApp(routes, "/users");
    expect(await screen.findByText("Your role cannot see this tenant's users.")).toBeInTheDocument();
    expect(screen.queryByText("not permitted")).not.toBeInTheDocument();
  });
});

describe("adding a user", () => {
  const PROVISION = "http://localhost:3000/atsapbx.v1.IdentityService/ProvisionPrincipal";

  it("is offered to administrators only", async () => {
    const { signedIn } = await import("../../test/session");
    renderApp(routes, "/users", signedIn({ memberships: [{ ...tenantA, roles: ["AGENT"] }] }));
    await screen.findByRole("heading", { level: 1, name: "Users" });
    expect(screen.queryByRole("button", { name: "Add user" })).not.toBeInTheDocument();
  });

  it("adds a person once, refreshes the list, and keeps no password", async () => {
    const bodies: unknown[] = [];
    let lists = 0;
    server.use(
      http.post(OVERVIEW_URLS.principals, () => {
        lists++;
        return HttpResponse.json({ principals: [ana] });
      }),
      http.post(PROVISION, async ({ request }) => {
        bodies.push(await request.json());
        return HttpResponse.json({ principal: { principalId: "p-new" } });
      }),
    );
    const user = userEvent.setup();
    const { queryClient } = renderApp(routes, "/users");
    await user.click(await screen.findByRole("button", { name: "Add user" }));
    const dialog = within(screen.getByRole("dialog", { name: "Add a user" }));
    expect(dialog.getByLabelText("Initial password (optional)")).toHaveAccessibleDescription(
      "Leave empty to invite a new person by email. Someone who already has an account keeps their own password.",
    );
    await user.type(dialog.getByLabelText("Email"), "cara@example.com");
    await user.type(dialog.getByLabelText("Name"), "Cara Ndi");
    await user.selectOptions(dialog.getByLabelText("Role"), "SUPERVISOR");
    await user.type(dialog.getByLabelText("Initial password (optional)"), "first password 1");
    await user.click(dialog.getByRole("button", { name: "Add user" }));

    expect(await screen.findByText("Cara Ndi added to this tenant.")).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(bodies).toEqual([{ tenantId: tenantA.tenantId, email: "cara@example.com", displayName: "Cara Ndi", role: "SUPERVISOR", password: "first password 1" }]);
    expect(lists).toBeGreaterThan(1);
    expect(JSON.stringify(queryClient.getMutationCache().getAll().map((m) => m.state.variables))).not.toContain("first password 1");
    expect(document.body.textContent).not.toContain("first password 1");
  });

  it("without a password, nothing is sent for it (the server invites by email)", async () => {
    let body: Record<string, unknown> = {};
    server.use(http.post(PROVISION, async ({ request }) => {
      body = (await request.json()) as Record<string, unknown>;
      return HttpResponse.json({ principal: {} });
    }));
    const user = userEvent.setup();
    renderApp(routes, "/users");
    await user.click(await screen.findByRole("button", { name: "Add user" }));
    const dialog = within(screen.getByRole("dialog"));
    await user.type(dialog.getByLabelText("Email"), "dan@example.com");
    await user.type(dialog.getByLabelText("Name"), "Dan");
    await user.click(dialog.getByRole("button", { name: "Add user" }));
    await screen.findByText("Dan added to this tenant.");
    expect(body).not.toHaveProperty("password");
  });

  it("checks the form before sending, and explains an existing member at the email", async () => {
    let sent = 0;
    server.use(http.post(PROVISION, () => {
      sent++;
      return HttpResponse.json({ code: "already_exists", message: "already a member of this tenant" }, { status: 409 });
    }));
    const user = userEvent.setup();
    renderApp(routes, "/users");
    await user.click(await screen.findByRole("button", { name: "Add user" }));
    const dialog = within(screen.getByRole("dialog"));
    await user.click(dialog.getByRole("button", { name: "Add user" }));
    expect(dialog.getByLabelText("Email")).toHaveAccessibleDescription(/Enter an email address/);
    expect(dialog.getByLabelText("Name")).toHaveAccessibleDescription("Enter the person's name.");
    await user.type(dialog.getByLabelText("Initial password (optional)"), "short");
    await user.type(dialog.getByLabelText("Email"), "ana@example.com");
    await user.type(dialog.getByLabelText("Name"), "Ana");
    await user.click(dialog.getByRole("button", { name: "Add user" }));
    expect(dialog.getByLabelText(/Initial password/)).toHaveAccessibleDescription(/Use at least 8 characters/);
    expect(sent).toBe(0);

    await user.clear(dialog.getByLabelText(/Initial password/));
    await user.click(dialog.getByRole("button", { name: "Add user" }));
    expect(await dialog.findByText("This person is already a member of this tenant.")).toBeInTheDocument();
    expect(sent).toBe(1);
  });

  it("offers the platform-administrator role only to a platform administrator", async () => {
    const user = userEvent.setup();
    renderApp(routes, "/users");
    await user.click(await screen.findByRole("button", { name: "Add user" }));
    const options = within(screen.getByLabelText("Role")).getAllByRole("option").map((o) => o.getAttribute("value"));
    expect(options).toEqual(["AGENT", "SUPERVISOR", "TENANT_ADMIN"]);
  });
});

describe("roles", () => {
  const GRANT = "http://localhost:3000/atsapbx.v1.IdentityService/GrantRole";
  const REVOKE = "http://localhost:3000/atsapbx.v1.IdentityService/RevokeRole";

  function listing() {
    return http.post(OVERVIEW_URLS.principals, () => HttpResponse.json({ principals: [ana, ben] }));
  }

  it("grants only a role the person does not hold, at its scope", async () => {
    let body: unknown;
    server.use(listing(), http.post(GRANT, async ({ request }) => {
      body = await request.json();
      return HttpResponse.json({});
    }));
    const user = userEvent.setup();
    renderApp(routes, "/users");
    await user.click(await screen.findByRole("button", { name: "Actions for Ben Tabi" }));
    await user.click(screen.getByRole("menuitem", { name: "Grant a role…" }));
    const dialog = within(screen.getByRole("dialog", { name: "Grant a role to Ben Tabi" }));
    expect(dialog.getAllByRole("option").map((o) => o.getAttribute("value"))).toEqual(["AGENT", "TENANT_ADMIN"]);
    await user.selectOptions(dialog.getByLabelText("Role"), "TENANT_ADMIN");
    await user.click(dialog.getByRole("button", { name: "Grant role" }));
    expect(await screen.findByText("Tenant administrator granted to Ben Tabi.")).toBeInTheDocument();
    expect(body).toEqual({ tenantId: tenantA.tenantId, principalId: "p-ben", role: "TENANT_ADMIN", scope: "tenant" });
  });

  it("revokes after a confirmation that names the role and the person", async () => {
    let body: unknown;
    server.use(listing(), http.post(REVOKE, async ({ request }) => {
      body = await request.json();
      return HttpResponse.json({});
    }));
    const user = userEvent.setup();
    renderApp(routes, "/users");
    await user.click(await screen.findByRole("button", { name: "Actions for Ben Tabi" }));
    await user.click(screen.getByRole("menuitem", { name: "Revoke Supervisor" }));
    const confirm = within(screen.getByRole("dialog", { name: "Revoke Supervisor from Ben Tabi?" }));
    expect(confirm.getByRole("button", { name: "Cancel" })).toHaveFocus(); // destructive: the safe choice first
    await user.click(confirm.getByRole("button", { name: "Revoke role" }));
    expect(await screen.findByText("Supervisor revoked from Ben Tabi.")).toBeInTheDocument();
    expect(body).toEqual({ tenantId: tenantA.tenantId, principalId: "p-ben", role: "SUPERVISOR", scope: "tenant" });
  });

  it("explains in plain words that the last administrator cannot be removed", async () => {
    server.use(listing(), http.post(REVOKE, () =>
      HttpResponse.json({ code: "failed_precondition", message: "cannot revoke the last tenant administrator" }, { status: 400 }),
    ));
    const user = userEvent.setup();
    renderApp(routes, "/users");
    await user.click(await screen.findByRole("button", { name: "Actions for Ana Mbe" }));
    await user.click(screen.getByRole("menuitem", { name: "Revoke Tenant administrator" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Revoke role" }));
    expect(await screen.findByText("Ana Mbe is this tenant's last administrator. Make someone else a tenant administrator first.")).toBeInTheDocument();
    expect(screen.queryByText("cannot revoke the last tenant administrator")).not.toBeInTheDocument();
    expect(screen.queryByText(/revoked from/)).not.toBeInTheDocument();
  });

  it("a non-administrator gets no row actions", async () => {
    const { signedIn } = await import("../../test/session");
    server.use(listing());
    renderApp(routes, "/users", signedIn({ memberships: [{ ...tenantA, roles: ["SUPERVISOR"] }] }));
    await screen.findByText("Ana Mbe");
    expect(screen.queryByRole("button", { name: /^Actions for/ })).not.toBeInTheDocument();
  });
});
