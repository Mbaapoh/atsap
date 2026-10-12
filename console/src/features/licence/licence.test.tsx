import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";
import { routes } from "../../app/routes";
import { LICENCE_STATUS_URL, server, validLicence } from "../../test/msw";
import { renderApp, signedIn, tenantA, TEST_BASE } from "../../test/session";
import { REVEAL_MS } from "./ApplyLicenceForm";

// console-foundation task 2.3: licence status and guarded activation.

const APPLY_URL = `${TEST_BASE}/atsapbx.v1.LicensingService/ApplyLicenseKey`;
const KEY = "eyJhbGciOiJFZERTQSJ9.c2VjcmV0LWxpY2VuY2UtcGF5bG9hZA.c2lnbmF0dXJl";
const platformAdmin = () => signedIn({ memberships: [{ ...tenantA, roles: ["PLATFORM_ADMIN"] }] });

function licence(status: Record<string, unknown>) {
  server.use(http.post(LICENCE_STATUS_URL, () => HttpResponse.json({ status: { ...validLicence, ...status } })));
}

afterEach(() => vi.useRealTimers());

describe("licence status", () => {
  it.each([
    [
      "Setup",
      { state: "LICENSE_STATE_NOT_ACTIVATED", condition: "LICENSE_CONDITION_SETUP", tier: "LICENSE_TIER_UNSPECIFIED", capacity: "CAPACITY_LEVEL_UNSPECIFIED", channels: 0, channelsInUse: 0, edition: "", reason: "not_activated" },
      /No licence is in force\. Calls cannot be made or received/,
    ],
    ["Free Community", { condition: "LICENSE_CONDITION_FREE_COMMUNITY", tier: "LICENSE_TIER_FREE_COMMUNITY", channels: 4, edition: "free" }, /Free Community licence is in force, with 4 channels/],
    ["Valid", {}, /The licence is in force\./],
    ["Capacity warning", { condition: "LICENSE_CONDITION_CAPACITY_WARNING", capacity: "CAPACITY_LEVEL_WARNING", channelsInUse: 13 }, /13 of 16 channels are in use/],
    ["Capacity reached", { condition: "LICENSE_CONDITION_CAPACITY_REACHED", capacity: "CAPACITY_LEVEL_REACHED", channelsInUse: 16 }, /All 16 channels are in use\. New calls are being declined; calls in progress continue/],
    ["Entitlement unverified", { state: "LICENSE_STATE_UNVERIFIED", condition: "LICENSE_CONDITION_ENTITLEMENT_UNVERIFIED" }, /could not be confirmed recently\. Nothing is restricted yet/],
    ["Degraded", { state: "LICENSE_STATE_DEGRADED", condition: "LICENSE_CONDITION_DEGRADED", channels: 4, reason: "expired" }, /Capacity has fallen to 4 channels\. The licence expired/],
    ["Licence unknown", { condition: "LICENSE_CONDITION_UNSPECIFIED" }, /could not be read/],
  ])("%s has its own headline and explanation", async (label, status, explanation) => {
    licence(status);
    renderApp(routes, "/licence");
    const headline = await screen.findByText(label, { selector: ".licence-condition" });
    expect(headline.closest(".licence-headline")).toHaveTextContent(explanation);
  });

  it("never calls a degraded installation valid", async () => {
    licence({ state: "LICENSE_STATE_DEGRADED", condition: "LICENSE_CONDITION_DEGRADED", channels: 4, reason: "tampered" });
    renderApp(routes, "/licence");
    await screen.findByText("Degraded", { selector: ".licence-condition" });
    expect(screen.queryByText("Valid")).not.toBeInTheDocument();
    expect(screen.getByText("The stored licence failed verification.", { selector: "dd" })).toBeInTheDocument();
  });

  it("shows every fact the server reports", async () => {
    licence({ maxTenants: 0, maxExtensions: 200, expiresAt: "2027-03-01T12:00:00Z", lastConfirmedAt: "2026-10-12T08:00:00Z" });
    renderApp(routes, "/licence");
    const list = (await screen.findByText("Tier", { selector: "dt" })).closest("dl");
    if (!list) throw new Error("no facts list");
    const facts = within(list);
    expect(facts.getByText("Commercial")).toBeInTheDocument();
    expect(facts.getByText("2 in use of 16")).toBeInTheDocument();
    expect(facts.getByText("Unlimited")).toBeInTheDocument();
    expect(facts.getByText("200")).toBeInTheDocument();
    expect(facts.getByText(/2027/)).toBeInTheDocument();
    expect(facts.queryByText("Does not expire")).not.toBeInTheDocument();
  });

  it("never shows a reason code it does not know", async () => {
    licence({ state: "LICENSE_STATE_DEGRADED", condition: "LICENSE_CONDITION_DEGRADED", reason: "db_row_7f3_checksum" });
    renderApp(routes, "/licence");
    await screen.findByText("Degraded", { selector: ".licence-condition" });
    expect(document.body.textContent).not.toContain("db_row_7f3_checksum");
  });

  it("the top-bar pill shows the server's condition on every page", async () => {
    licence({ condition: "LICENSE_CONDITION_CAPACITY_REACHED", capacity: "CAPACITY_LEVEL_REACHED", channelsInUse: 16 });
    renderApp(routes, "/users");
    expect(await screen.findByRole("link", { name: /Licence: Capacity reached/ })).toBeInTheDocument();
  });

  it("the pill says unknown, not valid, when the status cannot be read", async () => {
    server.use(http.post(LICENCE_STATUS_URL, () => HttpResponse.json({ code: "unavailable", message: "x" }, { status: 503 })));
    renderApp(routes, "/users");
    expect(await screen.findByRole("link", { name: /Licence: Licence unknown/ })).toBeInTheDocument();
  });
});

describe("Setup gate", () => {
  const setup = { state: "LICENSE_STATE_NOT_ACTIVATED", condition: "LICENSE_CONDITION_SETUP", tier: "LICENSE_TIER_UNSPECIFIED", capacity: "CAPACITY_LEVEL_UNSPECIFIED", channels: 0 };

  it.each([
    ["/extensions", "Extensions"],
    ["/trunks", "Trunks and routes"],
  ])("in Setup, %s leads to the licence page with the reason", async (path, area) => {
    licence(setup);
    const { router } = renderApp(routes, path);
    expect(await screen.findByText(`${area} open once a licence is in force`)).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/licence");
    expect(screen.getByText("Ask your platform administrator to apply a licence.")).toBeInTheDocument();
  });

  it.each([
    ["Degraded", "LICENSE_CONDITION_DEGRADED"],
    ["Entitlement unverified", "LICENSE_CONDITION_ENTITLEMENT_UNVERIFIED"],
    ["Capacity reached", "LICENSE_CONDITION_CAPACITY_REACHED"],
  ])("%s stays administrable", async (_label, condition) => {
    licence({ condition });
    const { router } = renderApp(routes, "/extensions");
    expect(await screen.findByRole("heading", { level: 1, name: "Extensions" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/extensions");
  });

  it("does not gate on a status it cannot read: the server decides", async () => {
    server.use(http.post(LICENCE_STATUS_URL, () => HttpResponse.json({ code: "unavailable", message: "x" }, { status: 503 })));
    renderApp(routes, "/extensions");
    expect(await screen.findByRole("heading", { level: 1, name: "Extensions" })).toBeInTheDocument();
  });

  it("users and call history stay open in Setup", async () => {
    licence(setup);
    renderApp(routes, "/users");
    expect(await screen.findByRole("heading", { level: 1, name: "Users" })).toBeInTheDocument();
  });
});

describe("applying a licence", () => {
  it("is offered to a platform administrator only", async () => {
    renderApp(routes, "/licence");
    await screen.findByText("Valid", { selector: ".licence-condition" });
    expect(screen.queryByRole("heading", { name: "Apply licence" })).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Licence key")).not.toBeInTheDocument();
  });

  it("masks the key, confirms, applies, refreshes the status in place, and forgets the key", async () => {
    let applied: unknown;
    let condition = "LICENSE_CONDITION_SETUP";
    server.use(
      http.post(LICENCE_STATUS_URL, () => HttpResponse.json({ status: { ...validLicence, condition, state: condition.endsWith("SETUP") ? "LICENSE_STATE_NOT_ACTIVATED" : "LICENSE_STATE_VALID" } })),
      http.post(APPLY_URL, async ({ request }) => {
        applied = await request.json();
        condition = "LICENSE_CONDITION_VALID";
        return HttpResponse.json({ status: { ...validLicence } });
      }),
    );
    const user = userEvent.setup();
    const { queryClient } = renderApp(routes, "/licence", platformAdmin());

    await screen.findByText("Setup", { selector: ".licence-condition" });
    const input = screen.getByLabelText("Licence key");
    expect(input).toHaveAttribute("type", "password");
    expect(input).toHaveAttribute("autocomplete", "off");
    await user.click(input);
    await user.paste(KEY);
    await user.click(screen.getByRole("button", { name: "Apply licence" }));

    const dialog = await screen.findByRole("dialog", { name: "Apply this licence?" });
    expect(dialog).not.toHaveTextContent(KEY);
    await user.click(within(dialog).getByRole("button", { name: "Apply licence" }));

    expect(await screen.findByText("Licence applied.")).toBeInTheDocument();
    expect(applied).toEqual({ token: KEY });
    expect(await screen.findByText("Valid", { selector: ".licence-condition" })).toBeInTheDocument();
    expect(screen.getByLabelText("Licence key")).toHaveValue("");
    // The key is nowhere the page, the cache or the browser keeps.
    expect(document.documentElement.outerHTML).not.toContain(KEY);
    expect(JSON.stringify(queryClient.getQueryCache().getAll().map((q) => q.state.data))).not.toContain(KEY);
    expect(queryClient.getMutationCache().getAll().map((m) => m.state.variables)).not.toContain(KEY);
    expect(localStorage.length + sessionStorage.length).toBe(0);
    expect(window.location.href).not.toContain(KEY);
  });

  it("reveals the key only on request, and masks it again after a while", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    renderApp(routes, "/licence", platformAdmin());

    const input = await screen.findByLabelText("Licence key");
    await user.click(input);
    await user.paste(KEY);
    await user.click(screen.getByRole("button", { name: "Show" }));
    expect(input).toHaveAttribute("type", "text");
    expect(screen.getByRole("button", { name: "Hide" })).toHaveAttribute("aria-pressed", "true");

    act(() => {
      vi.advanceTimersByTime(REVEAL_MS);
    });
    expect(input).toHaveAttribute("type", "password");
  });

  it("cancelling the confirmation sends nothing", async () => {
    let sent = false;
    server.use(
      http.post(APPLY_URL, () => {
        sent = true;
        return HttpResponse.json({});
      }),
    );
    const user = userEvent.setup();
    renderApp(routes, "/licence", platformAdmin());

    await user.click(await screen.findByLabelText("Licence key"));
    await user.paste(KEY);
    await user.click(screen.getByRole("button", { name: "Apply licence" }));
    await user.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Cancel" }));

    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(sent).toBe(false);
  });

  it("a key that does not verify is explained at the field, without echoing it", async () => {
    server.use(http.post(APPLY_URL, () => HttpResponse.json({ code: "invalid_argument", message: "licence token does not verify" }, { status: 400 })));
    const user = userEvent.setup();
    renderApp(routes, "/licence", platformAdmin());

    await user.click(await screen.findByLabelText("Licence key"));
    await user.paste(KEY);
    await user.click(screen.getByRole("button", { name: "Apply licence" }));
    await user.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Apply licence" }));

    await waitFor(() => expect(screen.getByLabelText("Licence key")).toHaveAccessibleDescription(/This key did not verify/));
    expect(document.documentElement.outerHTML).not.toContain(KEY);
    expect(screen.queryByText(/does not verify$/)).not.toBeInTheDocument();
    expect(screen.getByText("Valid", { selector: ".licence-condition" })).toBeInTheDocument();
  });

  it("a server refusal of the scope is shown plainly", async () => {
    server.use(http.post(APPLY_URL, () => HttpResponse.json({ code: "permission_denied", message: "not permitted" }, { status: 403 })));
    const user = userEvent.setup();
    renderApp(routes, "/licence", platformAdmin());

    await user.click(await screen.findByLabelText("Licence key"));
    await user.paste(KEY);
    await user.click(screen.getByRole("button", { name: "Apply licence" }));
    await user.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Apply licence" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Only a platform administrator can apply a licence.");
  });

  it("asks for a key before confirming", async () => {
    const user = userEvent.setup();
    renderApp(routes, "/licence", platformAdmin());
    await user.click(await screen.findByRole("button", { name: "Apply licence" }));
    expect(screen.getByLabelText("Licence key")).toHaveAccessibleDescription(/Paste the licence key you were sent/);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("in Setup, a platform administrator is pointed at the form", async () => {
    licence({ condition: "LICENSE_CONDITION_SETUP", state: "LICENSE_STATE_NOT_ACTIVATED" });
    renderApp(routes, "/extensions", platformAdmin());
    expect(await screen.findByText("Apply a licence key below.")).toBeInTheDocument();
  });
});
