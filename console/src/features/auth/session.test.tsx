import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { createApiClients } from "../../api/clients";
import { createApiTransport } from "../../api/transport";
import { routes } from "../../app/routes";
import { server } from "../../test/msw";
import { renderApp, signedIn, tenantA, tenantB, TEST_BASE } from "../../test/session";
import { passwordProblem, SessionController } from "./adapter";
import { SessionProvider } from "./SessionProvider";
import { initialSession, SessionStore } from "./session";

const auth = (method: string) => `${TEST_BASE}/atsapbx.v1.AuthService/${method}`;
const signedOut = { ...initialSession, status: "signed-out" as const };
const PASSWORD = "correct horse battery";

const wireMember = (m: typeof tenantA) => ({ tenantId: m.tenantId, tenantName: m.tenantName, principalId: m.principalId, roles: m.roles });
const access = (m: typeof tenantA, token: string) => ({ token, tenantId: m.tenantId, principalId: m.principalId, expiresAt: "2030-01-01T00:00:00Z" });
const refused = () => HttpResponse.json({ code: "unauthenticated", message: "invalid credentials" }, { status: 401 });

function discoverPassword() {
  return http.post(auth("DiscoverSignIn"), () => HttpResponse.json({ method: "SIGN_IN_METHOD_PASSWORD" }));
}

// noCredentialLeft asserts the browser holds nothing a person could take
// away: no storage entry, no readable cookie, nothing in the address or
// history state.
function noCredentialLeft(...secrets: string[]) {
  expect(localStorage.length).toBe(0);
  expect(sessionStorage.length).toBe(0);
  expect(document.cookie).toBe("");
  const visible = window.location.href + JSON.stringify(window.history.state ?? null);
  for (const s of secrets) expect(visible).not.toContain(s);
}

async function signInAs(user: ReturnType<typeof userEvent.setup>, email = "ana@example.com", password = PASSWORD) {
  await user.type(screen.getByLabelText("Email"), email);
  await user.click(screen.getByRole("button", { name: "Continue" }));
  await user.type(await screen.findByLabelText("Password"), password);
  await user.click(screen.getByRole("button", { name: "Sign in" }));
}

beforeEach(() => {
  localStorage.clear();
  sessionStorage.clear();
});

describe("sign in", () => {
  it("discovers the method, signs in, and lands in the shell for the one Tenant", async () => {
    let body: unknown;
    server.use(
      discoverPassword(),
      http.post(auth("SignIn"), async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({ access: access(tenantA, "tok-1"), memberships: [wireMember(tenantA)] });
      }),
    );
    const user = userEvent.setup();
    const { router, store } = renderApp(routes, "/sign-in", signedOut);

    await signInAs(user);

    expect(await screen.findByRole("heading", { level: 1, name: "Overview" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/");
    expect(screen.getByText("Acme")).toBeInTheDocument();
    expect(body).toEqual({ email: "ana@example.com", password: PASSWORD });
    expect(store.accessToken()).toBe("tok-1");
    noCredentialLeft(PASSWORD, "tok-1");
  });

  it("returns to the page the guard sent the person away from", async () => {
    server.use(discoverPassword(), http.post(auth("SignIn"), () => HttpResponse.json({ access: access(tenantA, "t"), memberships: [wireMember(tenantA)] })));
    const user = userEvent.setup();
    const { router } = renderApp(routes, "/extensions", signedOut);

    expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeInTheDocument();
    await signInAs(user);

    expect(await screen.findByRole("heading", { level: 1, name: "Extensions" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/extensions");
  });

  it("answers a wrong password with one neutral message and clears the field", async () => {
    server.use(discoverPassword(), http.post(auth("SignIn"), refused));
    const user = userEvent.setup();
    renderApp(routes, "/sign-in", signedOut);

    await signInAs(user, "nobody@example.com", "wrong-password");

    expect(await screen.findByRole("alert")).toHaveTextContent("The email or password is not correct.");
    expect(screen.getByLabelText("Password")).toHaveValue("");
    expect(screen.queryByText(/invalid credentials/)).not.toBeInTheDocument();
  });

  it("holds the form for the server's Retry-After when throttled", async () => {
    server.use(
      discoverPassword(),
      http.post(auth("SignIn"), () =>
        HttpResponse.json({ code: "resource_exhausted", message: "throttled" }, { status: 429, headers: { "Retry-After": "30" } }),
      ),
    );
    const user = userEvent.setup();
    renderApp(routes, "/sign-in", signedOut);

    await signInAs(user);

    expect(await screen.findByRole("alert")).toHaveTextContent("Too many attempts. Try again in 30 seconds.");
    await user.type(screen.getByLabelText("Password"), "again");
    expect(screen.getByRole("button", { name: "Sign in" })).toBeDisabled();
  });

  it("explains SSO without offering a password or a sign-in button it cannot honour", async () => {
    server.use(
      http.post(auth("DiscoverSignIn"), () =>
        HttpResponse.json({ method: "SIGN_IN_METHOD_SSO", providerId: "x", providerDisplayName: "Contoso ID" }),
      ),
    );
    const user = userEvent.setup();
    renderApp(routes, "/sign-in", signedOut);

    await user.type(screen.getByLabelText("Email"), "ana@contoso.example");
    await user.click(screen.getByRole("button", { name: "Continue" }));

    expect(await screen.findByText("Your organization signs in with Contoso ID")).toBeInTheDocument();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
  });

  it("asks for a valid email before calling the server", async () => {
    const user = userEvent.setup();
    renderApp(routes, "/sign-in", signedOut);

    await user.type(screen.getByLabelText("Email"), "not-an-email");
    await user.click(screen.getByRole("button", { name: "Continue" }));

    expect(screen.getByLabelText("Email")).toHaveAccessibleDescription(/Enter an email address/);
  });
});

describe("Tenant selection", () => {
  it("asks a person with several memberships to choose, then acts in the chosen Tenant", async () => {
    let chosen: unknown;
    server.use(
      discoverPassword(),
      http.post(auth("SignIn"), () => HttpResponse.json({ memberships: [wireMember(tenantA), wireMember(tenantB)] })),
      http.post(auth("RefreshSession"), async ({ request }) => {
        chosen = await request.json();
        return HttpResponse.json({ access: access(tenantB, "tok-b"), memberships: [wireMember(tenantA), wireMember(tenantB)] });
      }),
    );
    const user = userEvent.setup();
    const { store } = renderApp(routes, "/sign-in", signedOut);

    await signInAs(user);
    expect(await screen.findByRole("heading", { level: 1, name: "Choose a tenant" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /Globex/ }));

    expect(await screen.findByRole("heading", { level: 1, name: "Overview" })).toBeInTheDocument();
    expect(chosen).toEqual({ tenantId: tenantB.tenantId });
    expect(store.accessToken()).toBe("tok-b");
    expect(screen.getByText("Globex")).toBeInTheDocument();
    noCredentialLeft(PASSWORD, "tok-b");
  });

  it("switching Tenant empties the query cache so no data crosses scopes", async () => {
    server.use(
      http.post(auth("RefreshSession"), () => HttpResponse.json({ access: access(tenantB, "tok-b"), memberships: [wireMember(tenantA), wireMember(tenantB)] })),
    );
    const user = userEvent.setup();
    const { queryClient } = renderApp(routes, "/", signedIn({ memberships: [tenantA, tenantB] }));
    queryClient.setQueryData(["extensions"], ["acme-only"]);

    await user.click(screen.getByRole("link", { name: "Switch tenant" }));
    await user.click(await screen.findByRole("button", { name: /Globex/ }));

    // Wait for the shell, not the word: the picker's own button says
    // "Globex" until navigation removes it.
    expect(await screen.findByRole("heading", { level: 1, name: "Overview" })).toBeInTheDocument();
    expect(screen.getByText("Globex", { selector: ".tenant-indicator strong" })).toBeInTheDocument();
    expect(queryClient.getQueryData(["extensions"])).toBeUndefined();
  });

  it("refused Tenant: says so and keeps the person where they were", async () => {
    server.use(http.post(auth("RefreshSession"), () => HttpResponse.json({ code: "permission_denied", message: "not permitted" }, { status: 403 })));
    const user = userEvent.setup();
    const { store } = renderApp(routes, "/select-tenant", signedIn({ memberships: [tenantA, tenantB] }));

    await user.click(await screen.findByRole("button", { name: /Globex/ }));

    expect(await screen.findByRole("alert")).toHaveTextContent("You can no longer act in that tenant.");
    expect(store.accessToken()).toBe("access-in-memory");
  });

  it("a person with no active membership is told so and can sign out", async () => {
    server.use(discoverPassword(), http.post(auth("SignIn"), () => HttpResponse.json({ memberships: [] })));
    const user = userEvent.setup();
    renderApp(routes, "/sign-in", signedOut);

    await signInAs(user);

    expect(await screen.findByRole("heading", { level: 1, name: "No active access" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Sign out" })).toBeInTheDocument();
  });
});

describe("refresh once after expiry", () => {
  const status = `${TEST_BASE}/atsapbx.v1.LicensingService/GetLicenseStatus`;

  function wired(state = signedIn()) {
    const store = new SessionStore(state);
    let controller: SessionController | undefined;
    const clients = createApiClients(
      createApiTransport({ baseUrl: TEST_BASE, accessToken: store.accessToken, refresh: () => controller?.refresh() ?? Promise.resolve(false) }),
    );
    controller = new SessionController(clients.auth, store);
    return { store, clients };
  }

  it("refreshes once and repeats the call with the new token", async () => {
    const seen: (string | null)[] = [];
    let refreshes = 0;
    server.use(
      http.post(status, ({ request }) => {
        const authz = request.headers.get("authorization");
        seen.push(authz);
        return authz === "Bearer fresh" ? HttpResponse.json({ status: { channels: 4 } }) : refused();
      }),
      http.post(auth("RefreshSession"), () => {
        refreshes++;
        return HttpResponse.json({ access: access(tenantA, "fresh"), memberships: [wireMember(tenantA)] });
      }),
    );
    const { clients, store } = wired();

    const res = await clients.licensing.getLicenseStatus({});

    expect(res.status?.channels).toBe(4);
    expect(refreshes).toBe(1);
    expect(seen).toEqual(["Bearer access-in-memory", "Bearer fresh"]);
    expect(store.get().status).toBe("signed-in");
  });

  it("shares one refresh between calls refused together", async () => {
    let refreshes = 0;
    server.use(
      http.post(status, ({ request }) => (request.headers.get("authorization") === "Bearer fresh" ? HttpResponse.json({ status: {} }) : refused())),
      http.post(auth("RefreshSession"), async () => {
        refreshes++;
        await new Promise((r) => setTimeout(r, 20));
        return HttpResponse.json({ access: access(tenantA, "fresh"), memberships: [wireMember(tenantA)] });
      }),
    );
    const { clients } = wired();

    await Promise.all([clients.licensing.getLicenseStatus({}), clients.licensing.getLicenseStatus({}), clients.licensing.getLicenseStatus({})]);

    expect(refreshes).toBe(1);
  });

  it("does not refresh twice: a second refusal ends the call", async () => {
    let refreshes = 0;
    server.use(
      http.post(status, refused),
      http.post(auth("RefreshSession"), () => {
        refreshes++;
        return HttpResponse.json({ access: access(tenantA, "fresh"), memberships: [wireMember(tenantA)] });
      }),
    );
    const { clients } = wired();

    await expect(clients.licensing.getLicenseStatus({})).rejects.toThrow();
    expect(refreshes).toBe(1);
  });

  it("a failed refresh clears the session and the guard returns to sign-in", async () => {
    server.use(http.post(status, refused), http.post(auth("RefreshSession"), refused));
    const { clients, store, router } = renderApp(routes, "/users");

    await act(async () => {
      await clients.licensing.getLicenseStatus({}).catch(() => undefined);
    });

    expect(store.get().status).toBe("signed-out");
    expect(store.accessToken()).toBeUndefined();
    expect(await screen.findByText("Your session has ended")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/sign-in");
  });

  it("never refreshes for a call made without a token", async () => {
    let refreshes = 0;
    server.use(
      http.post(auth("SignIn"), refused),
      http.post(auth("RefreshSession"), () => {
        refreshes++;
        return refused();
      }),
    );
    const { clients } = wired(signedOut);

    await expect(clients.auth.signIn({ email: "a@example.com", password: "x" })).rejects.toThrow();
    expect(refreshes).toBe(0);
  });
});

describe("restore on load", () => {
  function mount(store: SessionStore) {
    const clients = createApiClients(createApiTransport({ baseUrl: TEST_BASE, accessToken: store.accessToken }));
    const controller = new SessionController(clients.auth, store);
    render(
      <SessionProvider controller={controller}>
        <span />
      </SessionProvider>,
    );
  }

  it("restores a session from the refresh cookie", async () => {
    server.use(http.post(auth("RefreshSession"), () => HttpResponse.json({ access: access(tenantA, "restored"), memberships: [wireMember(tenantA)] })));
    const store = new SessionStore();
    mount(store);
    await waitFor(() => expect(store.get().status).toBe("signed-in"));
    expect(store.accessToken()).toBe("restored");
  });

  it("is signed out, quietly, when there is no session to restore", async () => {
    server.use(http.post(auth("RefreshSession"), refused));
    const store = new SessionStore();
    mount(store);
    await waitFor(() => expect(store.get().status).toBe("signed-out"));
    expect(store.get().endedReason).toBeNull();
  });
});

describe("sign out", () => {
  it("ends the session on the server with the bearer token, then forgets it", async () => {
    let authz: string | null = null;
    server.use(
      http.post(auth("SignOut"), ({ request }) => {
        authz = request.headers.get("authorization");
        return HttpResponse.json({});
      }),
    );
    const user = userEvent.setup();
    const { store, router, queryClient } = renderApp(routes, "/account");
    queryClient.setQueryData(["users"], ["someone"]);

    await user.click(screen.getByRole("button", { name: "Sign out" }));

    expect(await screen.findByRole("heading", { level: 1, name: "Sign in" })).toBeInTheDocument();
    expect(authz).toBe("Bearer access-in-memory");
    expect(store.accessToken()).toBeUndefined();
    expect(router.state.location.pathname).toBe("/sign-in");
    expect(queryClient.getQueryData(["users"])).toBeUndefined();
  });

  it("forgets the session even when the server cannot be reached", async () => {
    server.use(http.post(auth("SignOut"), () => HttpResponse.error()));
    const user = userEvent.setup();
    const { store } = renderApp(routes, "/account");

    await user.click(screen.getByRole("button", { name: "Sign out" }));

    await waitFor(() => expect(store.get().status).toBe("signed-out"));
  });
});

describe("must-change password", () => {
  it("reaches only the account page", async () => {
    const { router } = renderApp(routes, "/users", signedIn({ passwordChangeRequired: true }));
    expect(await screen.findByText("Change your password to continue")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/account");
  });
});

describe("password recovery", () => {
  afterEach(() => window.history.replaceState(null, "", "/"));

  it("requests a link and answers the same for every address", async () => {
    let body: unknown;
    server.use(
      http.post(auth("RequestPasswordReset"), async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({});
      }),
    );
    const user = userEvent.setup();
    renderApp(routes, "/forgot-password", signedOut);

    await user.type(screen.getByLabelText("Email"), "anyone@example.com");
    await user.click(screen.getByRole("button", { name: "Send reset link" }));

    expect(await screen.findByText(/If that address can sign in with a password/)).toBeInTheDocument();
    expect(body).toEqual({ email: "anyone@example.com" });
  });

  it("takes the secret from the link fragment, removes it from the address, and redeems it", async () => {
    window.history.replaceState(null, "", "/reset#s3cret-from-email");
    let body: unknown;
    server.use(
      http.post(auth("CompletePasswordReset"), async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({});
      }),
    );
    const user = userEvent.setup();
    renderApp(routes, "/reset", signedOut);

    await waitFor(() => expect(window.location.hash).toBe(""));
    expect(window.location.href).not.toContain("s3cret");
    await user.type(screen.getByLabelText("New password"), PASSWORD);
    await user.type(screen.getByLabelText("Confirm new password"), PASSWORD);
    await user.click(screen.getByRole("button", { name: "Set password" }));

    expect(await screen.findByText("Your password is set")).toBeInTheDocument();
    expect(body).toEqual({ secret: "s3cret-from-email", newPassword: PASSWORD });
    noCredentialLeft("s3cret", PASSWORD);
  });

  it("checks length and confirmation before sending", async () => {
    window.history.replaceState(null, "", "/reset#abc");
    const user = userEvent.setup();
    renderApp(routes, "/reset", signedOut);

    await user.type(screen.getByLabelText("New password"), "short");
    await user.click(screen.getByRole("button", { name: "Set password" }));
    expect(screen.getByLabelText("New password")).toHaveAccessibleDescription(/Use at least 8 characters/);

    await user.type(screen.getByLabelText("New password"), "long enough one");
    await user.type(screen.getByLabelText("Confirm new password"), "something else");
    await user.click(screen.getByRole("button", { name: "Set password" }));
    expect(screen.getByLabelText("Confirm new password")).toHaveAccessibleDescription("The passwords do not match.");
  });

  it("an expired or used link says so and offers a new one", async () => {
    window.history.replaceState(null, "", "/reset#used");
    // The server's answer for an unusable link (auth.rig.spec.ts proves it).
    server.use(
      http.post(auth("CompletePasswordReset"), () =>
        HttpResponse.json({ code: "invalid_argument", message: "this link is invalid or has expired" }, { status: 400 }),
      ),
    );
    const user = userEvent.setup();
    renderApp(routes, "/reset", signedOut);

    await user.type(screen.getByLabelText("New password"), PASSWORD);
    await user.type(screen.getByLabelText("Confirm new password"), PASSWORD);
    await user.click(screen.getByRole("button", { name: "Set password" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("This link has expired or was already used");
    expect(screen.getByRole("link", { name: "Request a new link" })).toBeInTheDocument();
  });

  it("counts password length the way the server does: by code point", () => {
    expect(passwordProblem("🔒🔒🔒🔒🔒🔒🔒🔒")).toBeNull();
    expect(passwordProblem("é".repeat(64))).toBeNull();
    expect(passwordProblem("é".repeat(65))).toBe("Use no more than 64 characters.");
  });

  it("an invitation link sets up the account", async () => {
    window.history.replaceState(null, "", "/invite#welcome");
    renderApp(routes, "/invite", signedOut);
    expect(await screen.findByRole("heading", { level: 1, name: "Set up your account" })).toBeInTheDocument();
  });
});
