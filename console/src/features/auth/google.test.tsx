import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { routes } from "../../app/routes";
import {
  BEGIN_GOOGLE_LINK_URL,
  BEGIN_GOOGLE_SIGN_IN_URL,
  COMPLETE_GOOGLE_LINK_URL,
  COMPLETE_GOOGLE_SIGN_IN_URL,
  SIGN_IN_OPTIONS_URL,
  server,
} from "../../test/msw";
import { renderApp, signedIn } from "../../test/session";
import { initialSession } from "./session";

const signedOut = { ...initialSession, status: "signed-out" as const };

describe("Google callback", () => {
  it("explains an incomplete callback", async () => {
    renderApp(routes, "/auth/google", signedOut);
    expect(await screen.findByText(/incomplete/)).toBeInTheDocument();
  });

  it("explains a cancelled consent screen", async () => {
    renderApp(routes, "/auth/google?error=access_denied", signedOut);
    expect(await screen.findByText("Google sign-in was cancelled.")).toBeInTheDocument();
  });

  it("explains another provider error", async () => {
    renderApp(routes, "/auth/google?error=server_error", signedOut);
    expect(await screen.findByText("Google could not complete the sign-in.")).toBeInTheDocument();
  });

  it("refuses an unlinked account like a wrong password", async () => {
    server.use(http.post(COMPLETE_GOOGLE_SIGN_IN_URL, () =>
      HttpResponse.json({ code: "unauthenticated", message: "no" }, { status: 401 })));
    renderApp(routes, "/auth/google?code=c&state=s", signedOut);
    expect(await screen.findByText("The email or password is not correct.")).toBeInTheDocument();
  });

  it("explains a failed link in a signed-in browser", async () => {
    server.use(http.post(COMPLETE_GOOGLE_LINK_URL, () =>
      HttpResponse.json({ code: "already_exists", message: "this Google account is already linked" }, { status: 409 })));
    renderApp(routes, "/auth/google?code=c&state=s", signedIn());
    expect(await screen.findByRole("alert")).toHaveTextContent("That already exists.");
  });
});

describe("Google entry points", () => {
  it("offers Google on the password step too", async () => {
    let begun: unknown;
    server.use(
      http.post(SIGN_IN_OPTIONS_URL, () => HttpResponse.json({ googleEnabled: true })),
      http.post(BEGIN_GOOGLE_SIGN_IN_URL, async ({ request }) => {
        begun = await request.json();
        return HttpResponse.json({ redirectUrl: "https://accounts.google.test/a" });
      }),
      http.post("http://localhost:3000/atsapbx.v1.AuthService/DiscoverSignIn", () => HttpResponse.json({ method: "SIGN_IN_METHOD_PASSWORD" })),
    );
    const user = userEvent.setup();
    renderApp(routes, "/sign-in", signedOut);
    await user.type(screen.getByLabelText("Email"), "ana@example.com");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await screen.findByLabelText("Password");
    await user.click(screen.getByRole("button", { name: "Continue with Google" }));
    await waitFor(() => expect(begun).toEqual({ returnTo: "/" }));
  });

  it("accepts an invitation with Google", async () => {
    window.history.replaceState(null, "", "/invite#welcome-secret");
    let begun: unknown;
    server.use(
      http.post(SIGN_IN_OPTIONS_URL, () => HttpResponse.json({ googleEnabled: true })),
      http.post(BEGIN_GOOGLE_SIGN_IN_URL, async ({ request }) => {
        begun = await request.json();
        return HttpResponse.json({ redirectUrl: "https://accounts.google.test/invite" });
      }),
    );
    const user = userEvent.setup();
    renderApp(routes, "/invite", signedOut);
    await user.click(await screen.findByRole("button", { name: "Accept with Google instead" }));
    await waitFor(() => expect(begun).toEqual({ returnTo: "/", inviteSecret: "welcome-secret" }));
  });

  it("reports a wrong password when linking", async () => {
    server.use(http.post(BEGIN_GOOGLE_LINK_URL, () => HttpResponse.json({ code: "unauthenticated", message: "no" }, { status: 401 })));
    const user = userEvent.setup();
    renderApp(routes, "/account", signedIn());
    await user.type(screen.getByLabelText("Your password"), "wrong-password");
    await user.click(screen.getByRole("button", { name: "Link Google account" }));
    expect(await screen.findByText("That password is not correct.")).toBeInTheDocument();
  });
});

describe("Google failure paths", () => {
  it("hides the button when the options call fails", async () => {
    server.use(http.post(SIGN_IN_OPTIONS_URL, () => HttpResponse.json({}, { status: 500 })));
    renderApp(routes, "/sign-in", signedOut);
    await screen.findByRole("heading", { level: 1, name: "Sign in" });
    await waitFor(() => expect(screen.queryByRole("button", { name: "Continue with Google" })).not.toBeInTheDocument());
  });

  it("explains a callback with a code but no state", async () => {
    renderApp(routes, "/auth/google?code=c", signedOut);
    expect(await screen.findByText(/incomplete/)).toBeInTheDocument();
  });

  it("shows a non-password link failure", async () => {
    server.use(http.post(BEGIN_GOOGLE_LINK_URL, () =>
      HttpResponse.json({ code: "permission_denied", message: "no" }, { status: 403 })));
    const user = userEvent.setup();
    renderApp(routes, "/account", signedIn());
    await user.type(screen.getByLabelText("Your password"), "a-password");
    await user.click(screen.getByRole("button", { name: "Link Google account" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("You do not have permission to do this.");
  });
});

describe("Google entry failure", () => {
  it("reports a Google redirect that cannot be started", async () => {
    server.use(
      http.post(SIGN_IN_OPTIONS_URL, () => HttpResponse.json({ googleEnabled: true })),
      http.post(BEGIN_GOOGLE_SIGN_IN_URL, () => HttpResponse.json({}, { status: 503 })),
    );
    const user = userEvent.setup();
    renderApp(routes, "/sign-in", signedOut);
    await user.click(await screen.findByRole("button", { name: "Continue with Google" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("The server could not be reached. Try again shortly.");
  });

  it("reports a callback with a state but no code", async () => {
    renderApp(routes, "/auth/google?state=s", signedOut);
    expect(await screen.findByText(/incomplete/)).toBeInTheDocument();
  });
});
