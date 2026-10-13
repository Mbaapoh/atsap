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
