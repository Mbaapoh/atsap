import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { routes } from "../../app/routes";
import { server } from "../../test/msw";
import { renderApp, signedIn, tenantA, TEST_BASE } from "../../test/session";

// console-foundation task 2.2: changing the password, the must-change
// gate, and telling the person when their session ends.

const auth = (method: string) => `${TEST_BASE}/atsapbx.v1.AuthService/${method}`;
const OLD = "the old passphrase";
const NEW = "a brand new passphrase";
const member = { tenantId: tenantA.tenantId, tenantName: tenantA.tenantName, principalId: tenantA.principalId, roles: tenantA.roles };
const renewed = (passwordChangeRequired = false) =>
  HttpResponse.json({
    access: { token: "renewed", tenantId: tenantA.tenantId, principalId: tenantA.principalId, expiresAt: "2030-01-01T00:00:00Z" },
    memberships: [member],
    passwordChangeRequired,
  });

async function fillChange(user: ReturnType<typeof userEvent.setup>, current: string, next: string, confirm = next) {
  const currentLabel = screen.queryByLabelText("Temporary password") ? "Temporary password" : "Current password";
  if (current) await user.type(screen.getByLabelText(currentLabel), current);
  if (next) await user.type(screen.getByLabelText("New password"), next);
  if (confirm) await user.type(screen.getByLabelText("Confirm new password"), confirm);
  await user.click(screen.getByRole("button", { name: "Change password" }));
}

describe("change password", () => {
  it("sends the current and new password, renews the session, and confirms", async () => {
    const calls: string[] = [];
    let body: unknown;
    server.use(
      http.post(auth("ChangePassword"), async ({ request }) => {
        calls.push("change");
        body = await request.json();
        return HttpResponse.json({});
      }),
      http.post(auth("RefreshSession"), () => {
        calls.push("refresh");
        return renewed();
      }),
    );
    const user = userEvent.setup();
    const { store, router } = renderApp(routes, "/account");

    await fillChange(user, OLD, NEW);

    expect(await screen.findByText("Password changed. Your other sessions have been signed out.")).toBeInTheDocument();
    expect(body).toEqual({ currentPassword: OLD, newPassword: NEW });
    expect(calls).toEqual(["change", "refresh"]);
    expect(store.accessToken()).toBe("renewed");
    expect(router.state.location.pathname).toBe("/account");
    expect(screen.getByLabelText("New password")).toHaveValue("");
  });

  it("a wrong current password is shown at that field and never resent", async () => {
    let changes = 0;
    let refreshes = 0;
    server.use(
      http.post(auth("ChangePassword"), () => {
        changes++;
        return HttpResponse.json({ code: "unauthenticated", message: "invalid credentials" }, { status: 401 });
      }),
      http.post(auth("RefreshSession"), () => {
        refreshes++;
        return renewed();
      }),
    );
    const user = userEvent.setup();
    const { store } = renderApp(routes, "/account");

    await fillChange(user, "wrong one", NEW);

    await waitFor(() => expect(screen.getByLabelText("Current password")).toHaveAccessibleDescription("The current password is not correct."));
    expect(changes).toBe(1);
    expect(refreshes).toBe(0);
    expect(store.get().status).toBe("signed-in");
  });

  it("renews a token about to expire before sending, rather than retrying after", async () => {
    const calls: string[] = [];
    server.use(
      http.post(auth("RefreshSession"), () => {
        calls.push("refresh");
        return renewed();
      }),
      http.post(auth("ChangePassword"), ({ request }) => {
        calls.push(`change:${request.headers.get("authorization")}`);
        return HttpResponse.json({});
      }),
    );
    const user = userEvent.setup();
    const soon = new Date(Date.now() + 10_000);
    renderApp(routes, "/account", signedIn({ access: { token: "old", expiresAt: soon, tenantId: tenantA.tenantId, principalId: tenantA.principalId } }));

    await fillChange(user, OLD, NEW);

    await screen.findByText("Password changed. Your other sessions have been signed out.");
    expect(calls.slice(0, 2)).toEqual(["refresh", "change:Bearer renewed"]);
  });

  it.each([
    ["", NEW, NEW, "Current password", "Enter your current password."],
    [OLD, "short", "short", "New password", "Use at least 8 characters."],
    [OLD, OLD, OLD, "New password", "Choose a password different from the current one."],
    [OLD, NEW, "something else", "Confirm new password", "The passwords do not match."],
  ])("checks before sending: current %j, new %j, confirm %j", async (current, next, confirm, field, message) => {
    let sent = false;
    server.use(
      http.post(auth("ChangePassword"), () => {
        sent = true;
        return HttpResponse.json({});
      }),
    );
    const user = userEvent.setup();
    renderApp(routes, "/account");

    await fillChange(user, current, next, confirm);

    expect(screen.getByLabelText(field)).toHaveAccessibleDescription(new RegExp(message.replace(/[.]/g, "\\.")));
    expect(sent).toBe(false);
  });
});

describe("must-change password", () => {
  it.each(["/", "/licence", "/users", "/extensions", "/trunks", "/calls", "/no-such-page"])("%s leads to the account page", async (path) => {
    const { router } = renderApp(routes, path, signedIn({ passwordChangeRequired: true }));
    expect(await screen.findByRole("heading", { level: 2, name: "Choose your own password" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/account");
  });

  it("changing it opens the rest of the console", async () => {
    server.use(http.post(auth("ChangePassword"), () => HttpResponse.json({})), http.post(auth("RefreshSession"), () => renewed(false)));
    const user = userEvent.setup();
    const { store, router } = renderApp(routes, "/account", signedIn({ passwordChangeRequired: true }));

    expect(screen.getByLabelText("Temporary password")).toBeInTheDocument();
    await fillChange(user, "temporary-from-admin", NEW);

    expect(await screen.findByRole("heading", { level: 1, name: "Overview" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/");
    expect(store.get().passwordChangeRequired).toBe(false);
  });

  it("can still sign out", async () => {
    server.use(http.post(auth("SignOut"), () => HttpResponse.json({})));
    const user = userEvent.setup();
    renderApp(routes, "/account", signedIn({ passwordChangeRequired: true }));

    await user.click(screen.getByRole("button", { name: "Sign out" }));

    expect(await screen.findByText("You have signed out")).toBeInTheDocument();
  });
});

describe("session-end feedback", () => {
  const endingIn = (ms: number) => signedIn({ sessionExpiresAt: new Date(Date.now() + ms) });

  it("warns five minutes before the session ends, with the time", async () => {
    renderApp(routes, "/", endingIn(3 * 60_000));
    expect(await screen.findByText(/^Your session ends at /)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Stay signed in" })).toBeInTheDocument();
  });

  it("stays quiet while the end is further off", async () => {
    renderApp(routes, "/", endingIn(30 * 60_000));
    await screen.findByRole("heading", { level: 1, name: "Overview" });
    expect(screen.queryByText(/^Your session ends at /)).not.toBeInTheDocument();
  });

  it("Stay signed in renews the session, and the warning goes when the server extends it", async () => {
    server.use(
      http.post(auth("RefreshSession"), () =>
        HttpResponse.json({
          access: { token: "renewed", tenantId: tenantA.tenantId, principalId: tenantA.principalId },
          memberships: [member],
          sessionExpiresAt: new Date(Date.now() + 30 * 60_000).toISOString(),
        }),
      ),
    );
    const user = userEvent.setup();
    renderApp(routes, "/", endingIn(3 * 60_000));

    await user.click(await screen.findByRole("button", { name: "Stay signed in" }));

    await waitFor(() => expect(screen.queryByText(/^Your session ends at /)).not.toBeInTheDocument());
  });

  it("once the time has passed, asks the server, and returns to sign-in with the reason", async () => {
    server.use(http.post(auth("RefreshSession"), () => HttpResponse.json({ code: "unauthenticated", message: "x" }, { status: 401 })));
    const { router } = renderApp(routes, "/users", endingIn(-1000));

    expect(await screen.findByText("Your session has ended")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/sign-in");
  });

  it("confirms a sign-out the person asked for", async () => {
    server.use(http.post(auth("SignOut"), () => HttpResponse.json({})));
    const user = userEvent.setup();
    renderApp(routes, "/account");

    await user.click(screen.getByRole("button", { name: "Sign out" }));

    expect(await screen.findByText("You have signed out")).toBeInTheDocument();
    expect(screen.queryByText("Your session has ended")).not.toBeInTheDocument();
  });
});
