import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { routes } from "../../app/routes";
import { HANGUP_CALL_URL, OVERVIEW_URLS, PLACE_CALL_URL, server } from "../../test/msw";
import { renderApp, signedIn, tenantA } from "../../test/session";

// console-click-to-call: placing and ending calls from Call history.

const registered = { extensionId: "e-1001", number: "1001", displayName: "Reception", deviceType: "DEVICE_TYPE_SIP", registration: "REGISTRATION_STATUS_REGISTERED" };
const unregistered = { extensionId: "e-1002", number: "1002", displayName: "Lab", deviceType: "DEVICE_TYPE_SIP", registration: "REGISTRATION_STATUS_NOT_REGISTERED" };
const live = { callId: "c3", direction: "INTERNAL", state: "Presenting", sourceNumber: "1001", destNumber: "1002", startedAt: "2026-10-12T11:00:00Z" };
const ended = { callId: "c4", direction: "OUTBOUND", state: "Terminated", sourceNumber: "1001", destNumber: "+44207", startedAt: "2026-10-12T10:00:00Z", durationSeconds: 30, terminationReason: "normal clearing" };

function tenantCalls(list: object[]) {
  return http.post(OVERVIEW_URLS.calls, () => HttpResponse.json({ calls: list }));
}

function extensions(list: object[]) {
  return http.post(OVERVIEW_URLS.extensions, () => HttpResponse.json({ extensions: list }));
}

describe("call actions", () => {
  it("places a call from a registered extension and never offers an unregistered one", async () => {
    const placed: Record<string, unknown>[] = [];
    server.use(
      tenantCalls([ended]),
      extensions([registered, unregistered]),
      http.post(PLACE_CALL_URL, async ({ request }) => {
        placed.push((await request.json()) as Record<string, unknown>);
        return HttpResponse.json({ callId: "c9" });
      }),
    );
    const user = userEvent.setup();
    renderApp(routes, "/calls");
    await screen.findByRole("table", { name: "Calls of this tenant" });

    await user.click(screen.getByRole("button", { name: "Place a call" }));
    const dialog = screen.getByRole("dialog", { name: "Place a call" });
    const from = within(dialog).getByLabelText("From extension");
    const options = within(from).getAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual(["1001 · Reception", "1002 · Lab — No device registered"]);
    expect(options[1]).toBeDisabled();

    await user.type(within(dialog).getByLabelText("Number to call"), "+237222000111");
    await user.click(within(dialog).getByRole("button", { name: "Call" }));

    expect(await screen.findByText("Calling +237222000111 — answer extension 1001 to connect.")).toBeInTheDocument();
    expect(placed).toEqual([{ tenantId: tenantA.tenantId, extensionId: "e-1001", destination: "+237222000111" }]);
    expect(screen.queryByRole("dialog", { name: "Place a call" })).not.toBeInTheDocument();
  });

  it("says in console words why a placement was refused, never the server's text", async () => {
    server.use(
      tenantCalls([ended]),
      extensions([registered]),
      http.post(PLACE_CALL_URL, () =>
        HttpResponse.json({ code: "failed_precondition", message: "destination is unroutable: no extension or outbound route matches it" }, { status: 400 }),
      ),
    );
    const user = userEvent.setup();
    renderApp(routes, "/calls");
    await screen.findByRole("table", { name: "Calls of this tenant" });
    await user.click(screen.getByRole("button", { name: "Place a call" }));
    const dialog = screen.getByRole("dialog", { name: "Place a call" });
    await user.type(within(dialog).getByLabelText("Number to call"), "999");
    await user.click(within(dialog).getByRole("button", { name: "Call" }));

    expect(await within(dialog).findByText("No extension or outbound route matches that number. Check it and try again.")).toBeInTheDocument();
    expect(dialog.textContent).not.toContain("unroutable");
  });

  it("hangs up a ringing or active call from its row, and offers nothing for a finished one", async () => {
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

    await user.click(table.getByRole("button", { name: "Actions for call to 1002" }));
    await user.click(screen.getByRole("menuitem", { name: "Hang up…" }));
    const confirm = screen.getByRole("dialog", { name: "Hang up the call with 1002?" });
    await user.click(within(confirm).getByRole("button", { name: "Hang up" }));

    expect(await screen.findByText("Call ended.")).toBeInTheDocument();
    expect(hung).toEqual([{ tenantId: tenantA.tenantId, callId: "c3" }]);

    const finished = table.getByRole("row", { name: /\+44207/ });
    expect(within(finished).queryByRole("button", { name: /Actions for/ })).not.toBeInTheDocument();
  });

  it.each([
    ["destination is unroutable: no extension or outbound route matches it", "No extension or outbound route matches that number. Check it and try again."],
    ["trunk capacity: every trunk for this destination is at its channel limit", "Every trunk for that number is at its channel limit. Try again shortly."],
    ["the calling extension has no registered device", "That extension has no registered device. Register a phone to it first."],
    ["call refused: over_capacity", "The platform refused this call. Check the number and the extension, then try again."],
    ["some new precondition", "This cannot be done in the current state. Reload and try again."],
  ])("says %j in console words, never echoing the server's text", async (raw, copy) => {
    server.use(
      tenantCalls([ended]),
      extensions([registered]),
      http.post(PLACE_CALL_URL, () => HttpResponse.json({ code: "failed_precondition", message: raw }, { status: 400 })),
    );
    const user = userEvent.setup();
    renderApp(routes, "/calls");
    await screen.findByRole("table", { name: "Calls of this tenant" });
    await user.click(screen.getByRole("button", { name: "Place a call" }));
    const dialog = screen.getByRole("dialog", { name: "Place a call" });
    await user.type(within(dialog).getByLabelText("Number to call"), "999");
    await user.click(within(dialog).getByRole("button", { name: "Call" }));

    expect(await within(dialog).findByText(copy)).toBeInTheDocument();
    expect(dialog.textContent).not.toContain(raw);
  });

  it("switches the calling extension and closes on Escape", async () => {
    const second = { ...registered, extensionId: "e-1004", number: "1004", displayName: "Desk" };
    server.use(tenantCalls([ended]), extensions([registered, second]));
    const user = userEvent.setup();
    renderApp(routes, "/calls");
    await screen.findByRole("table", { name: "Calls of this tenant" });
    await user.click(screen.getByRole("button", { name: "Place a call" }));
    const dialog = screen.getByRole("dialog", { name: "Place a call" });
    const from = within(dialog).getByLabelText("From extension");
    await user.selectOptions(from, "e-1004");
    expect(from).toHaveValue("e-1004");
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog", { name: "Place a call" })).not.toBeInTheDocument();
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
    const user = userEvent.setup();
    renderApp(routes, "/calls");
    await screen.findByRole("table", { name: "Calls of this tenant" });
    await user.click(screen.getByRole("button", { name: "Place a call" }));
    const dialog = screen.getByRole("dialog", { name: "Place a call" });
    await user.type(within(dialog).getByLabelText("Number to call"), "not a number");
    await user.click(within(dialog).getByRole("button", { name: "Call" }));

    expect(within(dialog).getByText("Use digits with an optional leading +, like +237222000111.")).toBeInTheDocument();
    expect(placed).toEqual([]);
  });

  it("explains when no extension has a registered device and disables Call", async () => {
    server.use(tenantCalls([ended]), extensions([unregistered, { ...unregistered, extensionId: "e-1003", number: "1003", displayName: "" }]));
    const user = userEvent.setup();
    renderApp(routes, "/calls");
    await screen.findByRole("table", { name: "Calls of this tenant" });
    await user.click(screen.getByRole("button", { name: "Place a call" }));
    const dialog = screen.getByRole("dialog", { name: "Place a call" });
    expect(within(dialog).getByText("Register a phone to an extension first. Only an extension with a device can place a call.")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Call" })).toBeDisabled();
    expect(within(dialog).getAllByRole("option").map((o) => o.textContent)).toEqual(["1002 · Lab — No device registered", "1003 — No device registered"]);

    // Even a form submit that bypasses the disabled button refuses to call.
    await user.type(within(dialog).getByLabelText("Number to call"), "1003");
    fireEvent.submit(dialog.querySelector("form")!);
    expect(await within(dialog).findByText("Choose an extension with a registered device. Register a phone to it first.")).toBeInTheDocument();
  });

  it("handles a tenant with no extensions at all", async () => {
    server.use(tenantCalls([ended]), extensions([]));
    const user = userEvent.setup();
    renderApp(routes, "/calls");
    await screen.findByRole("table", { name: "Calls of this tenant" });
    await user.click(screen.getByRole("button", { name: "Place a call" }));
    const dialog = screen.getByRole("dialog", { name: "Place a call" });
    expect(within(dialog).getByText("Register a phone to an extension first. Only an extension with a device can place a call.")).toBeInTheDocument();
    expect(within(dialog).queryAllByRole("option")).toEqual([]);

    await user.type(within(dialog).getByLabelText("Number to call"), "1001");
    fireEvent.submit(dialog.querySelector("form")!);
    expect(await within(dialog).findByText("Choose an extension with a registered device. Register a phone to it first.")).toBeInTheDocument();
  });

  it("shows an unknown direction verbatim and names a call with no destination by its source", async () => {
    const odd = { ...live, callId: "c5", direction: "MYSTERY", destNumber: "", sourceNumber: "1009" };
    server.use(tenantCalls([odd]), extensions([registered]));
    const user = userEvent.setup();
    renderApp(routes, "/calls");
    const table = within(await screen.findByRole("table", { name: "Calls of this tenant" }));
    expect(table.getByRole("row", { name: /MYSTERY/ })).toBeInTheDocument();
    await user.click(table.getByRole("button", { name: "Actions for call to 1009" }));
    await user.click(screen.getByRole("menuitem", { name: "Hang up…" }));
    expect(screen.getByRole("dialog", { name: "Hang up the call with 1009?" })).toBeInTheDocument();
  });

  it("reports a failed hang-up in the confirmation without closing it", async () => {
    server.use(
      tenantCalls([live]),
      extensions([registered]),
      http.post(HANGUP_CALL_URL, () => HttpResponse.json({ code: "permission_denied", message: "ps_endpoints" }, { status: 403 })),
    );
    const user = userEvent.setup();
    renderApp(routes, "/calls");
    const table = within(await screen.findByRole("table", { name: "Calls of this tenant" }));
    await user.click(table.getByRole("button", { name: "Actions for call to 1002" }));
    await user.click(screen.getByRole("menuitem", { name: "Hang up…" }));
    const confirm = screen.getByRole("dialog", { name: "Hang up the call with 1002?" });
    await user.click(within(confirm).getByRole("button", { name: "Hang up" }));

    expect(await within(confirm).findByText("You do not have permission to do this.")).toBeInTheDocument();
    expect(confirm.textContent).not.toContain("ps_endpoints");
  });

  it("hides placement and hang-up from a role without call.place", async () => {
    server.use(tenantCalls([live]), extensions([registered]));
    renderApp(routes, "/calls", signedIn({ memberships: [{ ...tenantA, roles: ["AGENT"] }] }));
    await screen.findByRole("table", { name: "Calls of this tenant" });
    expect(screen.queryByRole("button", { name: "Place a call" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Actions for/ })).not.toBeInTheDocument();
  });
});
