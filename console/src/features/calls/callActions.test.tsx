import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { routes } from "../../app/routes";
import { GET_CALL_URL, HANGUP_CALL_URL, OVERVIEW_URLS, PLACE_CALL_URL, server } from "../../test/msw";
import { renderApp, signedIn, tenantA } from "../../test/session";

// console-call-session: the live call session, dialer and row hang-up.

const registered = { extensionId: "e-1001", number: "1001", displayName: "Reception", deviceType: "DEVICE_TYPE_SIP", registration: "REGISTRATION_STATUS_REGISTERED" };
const unregistered = { extensionId: "e-1002", number: "1002", displayName: "Lab", deviceType: "DEVICE_TYPE_SIP", registration: "REGISTRATION_STATUS_NOT_REGISTERED" };
const live = { callId: "c3", direction: "INTERNAL", state: "Presenting", sourceNumber: "1001", destNumber: "1002", startedAt: "2026-10-12T11:00:00Z" };
const ended = { callId: "c4", direction: "OUTBOUND", state: "Terminated", sourceNumber: "1001", destNumber: "+44207", startedAt: "2026-10-12T10:00:00Z", durationSeconds: 30, terminationReason: "normal clearing" };

function tenantCalls(list: object[], onChange?: () => void) {
  return http.post(OVERVIEW_URLS.calls, () => {
    onChange?.();
    return HttpResponse.json({ calls: list });
  });
}

function extensions(list: object[]) {
  return http.post(OVERVIEW_URLS.extensions, () => HttpResponse.json({ extensions: list }));
}

function callState(source: { state: string; caller: string; reason?: string }) {
  return {
    callId: "c9",
    state: source.state,
    startedAt: "2026-11-02T20:00:00Z",
    answeredAt: source.state === "Active" || source.state === "Terminated" ? "2026-11-02T20:00:05Z" : undefined,
    endedAt: source.state === "Terminated" ? "2026-11-02T20:00:12Z" : undefined,
    terminationReason: source.reason,
    participants: [
      { participantId: "p1", role: "CALLER", state: source.caller },
      { participantId: "p2", role: "AGENT", state: "Invited" },
    ],
  };
}

// getCallMock is mutable so a test can move the call through its states;
// the provider polls every second.
function getCallMock(initial: ReturnType<typeof callState>) {
  let current = initial;
  return {
    handler: http.post(GET_CALL_URL, () => HttpResponse.json(current)),
    set(next: ReturnType<typeof callState>) {
      current = next;
    },
  };
}

async function openDialer() {
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Place a call" }));
  const element = screen.getByRole("dialog", { name: "New call" });
  return { user, dialog: element, element };
}

describe("call session and dialer", () => {
  it("places a call, watches it ring, cancels it and shows the reason", async () => {
    const placed: Record<string, unknown>[] = [];
    const hung: Record<string, unknown>[] = [];
    const get = getCallMock(callState({ state: "Presenting", caller: "Invited" }));
    server.use(
      tenantCalls([ended]),
      extensions([registered, unregistered]),
      get.handler,
      http.post(PLACE_CALL_URL, async ({ request }) => {
        placed.push((await request.json()) as Record<string, unknown>);
        return HttpResponse.json({ callId: "c9" });
      }),
      http.post(HANGUP_CALL_URL, async ({ request }) => {
        hung.push((await request.json()) as Record<string, unknown>);
        get.set(callState({ state: "Terminated", caller: "Disconnected", reason: "api_hangup" }));
        return HttpResponse.json({});
      }),
    );
    renderApp(routes, "/calls");
    await screen.findByRole("table", { name: "Calls of this tenant" });

    const { user, dialog } = await openDialer();
    const options = within(within(dialog).getByLabelText("From extension")).getAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual(["1001 · Reception", "1002 · Lab — No device registered"]);
    expect(options[1]).toBeDisabled();
    await user.type(within(dialog).getByLabelText("Number to call"), "+237222000111");
    await user.click(within(dialog).getByRole("button", { name: "Call" }));

    expect(await screen.findByText("Ringing your extension 1001")).toBeInTheDocument();
    expect(screen.getByText("Answer your phone to continue. +237222000111 is not dialled yet.")).toBeInTheDocument();
    expect(screen.queryByRole("dialog", { name: "New call" })).not.toBeInTheDocument();
    expect(placed).toEqual([{ tenantId: tenantA.tenantId, extensionId: "e-1001", destination: "+237222000111" }]);

    await user.click(screen.getByRole("button", { name: /Cancel call/ }));
    expect(await screen.findByText("Call ended — ended from the console")).toBeInTheDocument();
    expect(hung).toEqual([{ tenantId: tenantA.tenantId, callId: "c9" }]);
    await user.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(screen.queryByText("Call ended — ended from the console")).not.toBeInTheDocument();
  });

  it("moves through the caller-first states and reports caller_no_answer", async () => {
    const get = getCallMock(callState({ state: "Presenting", caller: "Invited" }));
    server.use(
      tenantCalls([ended]),
      extensions([registered]),
      get.handler,
      http.post(PLACE_CALL_URL, () => HttpResponse.json({ callId: "c9" })),
    );
    renderApp(routes, "/calls");
    const { user, dialog } = await openDialer();
    await user.type(within(dialog).getByLabelText("Number to call"), "1002");
    await user.click(within(dialog).getByRole("button", { name: "Call" }));
    await screen.findByText("Ringing your extension 1001");

    get.set(callState({ state: "Presenting", caller: "Connected" }));
    expect(await screen.findByText("Calling 1002…", {}, { timeout: 4000 })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Hang up call with 1002" })).toBeInTheDocument();

    get.set(callState({ state: "Active", caller: "Connected" }));
    expect(await screen.findByText("In call with 1002", {}, { timeout: 4000 })).toBeInTheDocument();

    get.set(callState({ state: "Terminated", caller: "Disconnected", reason: "caller_no_answer" }));
    expect(await screen.findByText("Call ended — 1001 did not answer", {}, { timeout: 4000 })).toBeInTheDocument();
    expect(screen.getByText("1002 was never dialled.")).toBeInTheDocument();
  });

  it("recovers a live session after a reload", async () => {
    sessionStorage.setItem(
      `atsapbx.call-session.${tenantA.tenantId}`,
      JSON.stringify({ callId: "c9", from: { id: "e-1001", number: "1001", name: "Reception" }, destination: "1002", startedAt: "2026-11-02T20:00:00.000Z" }),
    );
    server.use(
      tenantCalls([live]),
      extensions([registered]),
      http.post(GET_CALL_URL, () => HttpResponse.json(callState({ state: "Active", caller: "Connected" }))),
    );
    renderApp(routes, "/calls");
    expect(await screen.findByText("In call with 1002")).toBeInTheDocument();
    const bar = screen.getByRole("region", { name: "Current call" });
    expect(within(bar).getByRole("button", { name: "Hang up call with 1002" })).toBeInTheDocument();
  });

  it("refreshes Call history while a call is in flight", async () => {
    const get = getCallMock(callState({ state: "Presenting", caller: "Invited" }));
    let reads = 0;
    server.use(
      tenantCalls([ended], () => {
        reads += 1;
      }),
      extensions([registered]),
      get.handler,
      http.post(PLACE_CALL_URL, () => HttpResponse.json({ callId: "c9" })),
    );
    renderApp(routes, "/calls");
    const { user, dialog } = await openDialer();
    await user.type(within(dialog).getByLabelText("Number to call"), "1002");
    await user.click(within(dialog).getByRole("button", { name: "Call" }));
    await screen.findByText("Ringing your extension 1001");
    await waitFor(() => expect(reads).toBeGreaterThanOrEqual(2), { timeout: 4000 });
    expect(screen.getByText(/Live · updated/)).toBeInTheDocument();
  });

  it("hangs up a ringing or active row directly, and offers nothing for a finished one", async () => {
    const hung: Record<string, unknown>[] = [];
    server.use(
      tenantCalls([live, ended]),
      extensions([registered]),
      http.post(HANGUP_CALL_URL, async ({ request }) => {
        hung.push((await request.json()) as Record<string, unknown>);
        return HttpResponse.json({});
      }),
    );
    const user = userEvent.setup();
    renderApp(routes, "/calls");
    const table = within(await screen.findByRole("table", { name: "Calls of this tenant" }));

    await user.click(table.getByRole("button", { name: "Hang up call with 1002" }));
    expect(await screen.findByText("Call ended.")).toBeInTheDocument();
    expect(hung).toEqual([{ tenantId: tenantA.tenantId, callId: "c3" }]);

    const finished = table.getByRole("row", { name: /\+44207/ });
    expect(within(finished).queryByRole("button", { name: /Hang up/ })).not.toBeInTheDocument();
  });

  it("says every placement refusal in console words, never the server's text", async () => {
    const cases: [string, string][] = [
      ["destination is unroutable: no extension or outbound route matches it", "No extension or outbound route matches that number. Check it and try again."],
      ["trunk capacity: every trunk for this destination is at its channel limit", "Every trunk for that number is at its channel limit. Try again shortly."],
      ["the calling extension has no registered device", "That extension has no registered device. Register a phone to it first."],
      ["call refused: over_capacity", "The platform refused this call. Check the number and the extension, then try again."],
      ["some new precondition", "This cannot be done in the current state. Reload and try again."],
    ];
    server.use(tenantCalls([ended]), extensions([registered]));
    renderApp(routes, "/calls");
    await screen.findByRole("table", { name: "Calls of this tenant" });

    for (const [raw, copy] of cases) {
      server.use(
        http.post(PLACE_CALL_URL, () => HttpResponse.json({ code: "failed_precondition", message: raw }, { status: 400 })),
      );
      const { user, dialog, element } = await openDialer();
      await user.type(within(dialog).getByLabelText("Number to call"), "999");
      await user.click(within(dialog).getByRole("button", { name: "Call" }));
      expect(await within(element).findByText(copy)).toBeInTheDocument();
      expect(element.textContent).not.toContain(raw);
      await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    }
  });

  it("does not send a number that is not digits", async () => {
    const placed: unknown[] = [];
    server.use(
      tenantCalls([ended]),
      extensions([registered]),
      http.post(PLACE_CALL_URL, async ({ request }) => {
        placed.push(await request.json());
        return HttpResponse.json({ callId: "c9" });
      }),
    );
    renderApp(routes, "/calls");
    await screen.findByRole("table", { name: "Calls of this tenant" });
    const { user, dialog } = await openDialer();
    await user.type(within(dialog).getByLabelText("Number to call"), "not a number");
    await user.click(within(dialog).getByRole("button", { name: "Call" }));
    expect(within(dialog).getByText("Use digits with an optional leading +, like +237222000111.")).toBeInTheDocument();
    expect(placed).toEqual([]);
  });

  it("explains when no extension has a registered device", async () => {
    server.use(tenantCalls([ended]), extensions([unregistered, { ...unregistered, extensionId: "e-1003", number: "1003", displayName: "" }]));
    renderApp(routes, "/calls");
    await screen.findByRole("table", { name: "Calls of this tenant" });
    const { dialog, element } = await openDialer();
    expect(within(dialog).getByText("Register a phone to an extension first. Only an extension with a device can place a call.")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Call" })).toBeDisabled();
    expect(within(dialog).getAllByRole("option").map((o) => o.textContent)).toEqual(["1002 · Lab — No device registered", "1003 — No device registered"]);
    await waitFor(() => expect(element.querySelector("select")).toBeTruthy());
    await userEvent.setup().type(within(dialog).getByLabelText("Number to call"), "1003");
    fireEvent.submit(element.querySelector("form")!);
    expect(await within(dialog).findByText("Choose an extension with a registered device. Register a phone to it first.")).toBeInTheDocument();
  });

  it("handles a tenant with no extensions at all", async () => {
    server.use(tenantCalls([ended]), extensions([]));
    renderApp(routes, "/calls");
    await screen.findByRole("table", { name: "Calls of this tenant" });
    const { dialog } = await openDialer();
    expect(within(dialog).getByText("Register a phone to an extension first. Only an extension with a device can place a call.")).toBeInTheDocument();
    expect(within(dialog).getAllByRole("option").map((o) => o.textContent)).toEqual(["No extensions"]);
  });

  it("shows an unknown direction verbatim and labels a call with no destination by its source", async () => {
    const odd = { ...live, callId: "c5", direction: "MYSTERY", destNumber: "", sourceNumber: "1009" };
    server.use(tenantCalls([odd]), extensions([registered]));
    renderApp(routes, "/calls");
    const table = within(await screen.findByRole("table", { name: "Calls of this tenant" }));
    expect(table.getByRole("row", { name: /MYSTERY/ })).toBeInTheDocument();
    expect(table.getByRole("button", { name: "Hang up call with 1009" })).toBeInTheDocument();
  });

  it("hides placement and hang-up from a role without call.place", async () => {
    server.use(tenantCalls([live]), extensions([registered]));
    renderApp(routes, "/calls", signedIn({ memberships: [{ ...tenantA, roles: ["AGENT"] }] }));
    await screen.findByRole("table", { name: "Calls of this tenant" });
    expect(screen.queryByRole("button", { name: "Place a call" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "New call" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Hang up/ })).not.toBeInTheDocument();
  });

  it("filters the extension list, offers recent numbers, and edits with the dial pad", async () => {
    const desk = { ...registered, extensionId: "e-1004", number: "1004", displayName: "Desk" };
    const inbound = { ...ended, callId: "c5", direction: "INBOUND", sourceNumber: "+237111", destNumber: "1001" };
    server.use(tenantCalls([ended, inbound, { ...ended, callId: "c6" }]), extensions([registered, unregistered, desk]));
    renderApp(routes, "/calls");
    await screen.findByRole("table", { name: "Calls of this tenant" });
    const { user, dialog } = await openDialer();

    await user.type(within(dialog).getByLabelText("Search extensions"), "Desk");
    const filtered = within(dialog).getByLabelText("From extension");
    expect(within(filtered).getAllByRole("option").map((o) => o.textContent)).not.toContain("1002 · Lab — No device registered");
    expect(within(filtered).getByRole("option", { name: "1004 · Desk" })).toBeInTheDocument();

    await user.clear(within(dialog).getByLabelText("Search extensions"));
    await user.selectOptions(within(dialog).getByLabelText("From extension"), "e-1004");
    expect(within(dialog).getByLabelText("From extension")).toHaveValue("e-1004");
    const recents = within(dialog).getByRole("group", { name: "Recent numbers" });
    expect(within(recents).getAllByRole("button").map((b) => b.textContent)).toEqual(["+44207"]);
    await user.click(within(recents).getByRole("button", { name: "+44207" }));
    expect(within(dialog).getByLabelText("Number to call")).toHaveValue("+44207");

    await user.clear(within(dialog).getByLabelText("Number to call"));
    const pad = within(dialog).getByRole("group", { name: "Dial pad" });
    await user.click(within(pad).getByRole("button", { name: "Add 1" }));
    await user.click(within(pad).getByRole("button", { name: "Add 0" }));
    await user.click(within(pad).getByRole("button", { name: "Add +" }));
    await user.click(within(pad).getByRole("button", { name: "Delete last digit" }));
    expect(within(dialog).getByLabelText("Number to call")).toHaveValue("10");
  });

  it("disables the global New call action while a call is live", async () => {
    sessionStorage.setItem(
      `atsapbx.call-session.${tenantA.tenantId}`,
      JSON.stringify({ callId: "c9", from: { id: "e-1001", number: "1001", name: "Reception" }, destination: "1002", startedAt: "2026-11-02T20:00:00.000Z" }),
    );
    server.use(
      tenantCalls([ended]),
      extensions([registered]),
      http.post(GET_CALL_URL, () => HttpResponse.json({ callId: "c9", state: "Presenting", terminated: false, participants: [{ role: "CALLER", state: "Invited" }] })),
    );
    renderApp(routes, "/calls");
    await screen.findByText("Ringing your extension 1001");
    expect(screen.getByRole("button", { name: "New call" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Place a call" })).toBeDisabled();
  });

  it("reports a failed row hang-up without pretending the call ended", async () => {
    server.use(
      tenantCalls([live]),
      extensions([registered]),
      http.post(HANGUP_CALL_URL, () => HttpResponse.json({ code: "permission_denied", message: "x" }, { status: 403 })),
    );
    const user = userEvent.setup();
    renderApp(routes, "/calls");
    const table = within(await screen.findByRole("table", { name: "Calls of this tenant" }));
    await user.click(table.getByRole("button", { name: "Hang up call with 1002" }));
    expect(await screen.findByText("Your role cannot end this call.")).toBeInTheDocument();
    expect(table.getByRole("row", { name: /1002/ })).toHaveTextContent("Presenting");
  });

  it("ticks an Active row's duration from its answer time", async () => {
    const answeredAt = new Date(Date.now() - 65_000).toISOString();
    const active = { ...live, callId: "c7", state: "Active", answeredAt };
    sessionStorage.setItem(
      `atsapbx.call-session.${tenantA.tenantId}`,
      JSON.stringify({ callId: "c9", from: { id: "e-1001", number: "1001", name: "Reception" }, destination: "1002", startedAt: "2026-11-02T20:00:00.000Z" }),
    );
    server.use(
      tenantCalls([active]),
      extensions([registered]),
      http.post(GET_CALL_URL, () => HttpResponse.json({ callId: "c9", state: "Active", startedAt: "2026-11-02T20:00:00Z", answeredAt: "2026-11-02T20:00:05Z", participants: [{ role: "CALLER", state: "Connected" }] })),
    );
    renderApp(routes, "/calls");
    const table = within(await screen.findByRole("table", { name: "Calls of this tenant" }));
    expect(table.getByRole("row", { name: /1002/ })).toHaveTextContent("1:05");
  });

  it("closes the dialer on Escape and explains an extension-load failure", async () => {
    server.use(tenantCalls([ended]), extensions([registered]));
    renderApp(routes, "/calls");
    await screen.findByRole("table", { name: "Calls of this tenant" });
    const { user } = await openDialer();
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog", { name: "New call" })).not.toBeInTheDocument();

    server.use(http.post(OVERVIEW_URLS.extensions, () => HttpResponse.json({ code: "permission_denied", message: "x" }, { status: 403 })));
    const again = await openDialer();
    expect(await within(again.dialog).findByText("Extensions could not be loaded")).toBeInTheDocument();
  });
});
