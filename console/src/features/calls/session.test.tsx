import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { routes } from "../../app/routes";
import { GET_CALL_URL, HANGUP_CALL_URL, OVERVIEW_URLS, server } from "../../test/msw";
import { renderApp, tenantA, tenantB } from "../../test/session";

// console-call-session: the shell call bar and its states, driven by a
// stored session and a mocked GetCall.

const KEY = `atsapbx.call-session.${tenantA.tenantId}`;
const stored = {
  callId: "c9",
  from: { id: "e-1001", number: "1001", name: "Reception" },
  destination: "1002",
  startedAt: "2026-11-02T20:00:00.000Z",
};

function startStored() {
  sessionStorage.setItem(KEY, JSON.stringify(stored));
}

function call(overrides: Record<string, unknown> = {}) {
  return {
    callId: "c9",
    state: "Presenting",
    startedAt: "2026-11-02T20:00:00Z",
    participants: [
      { participantId: "p1", role: "CALLER", state: "Invited" },
      { participantId: "p2", role: "AGENT", state: "Invited" },
    ],
    ...overrides,
  };
}

function bar() {
  return screen.getByRole("region", { name: "Current call" });
}

describe("call session bar", () => {
  it("shows Starting call until the first snapshot arrives", async () => {
    startStored();
    server.use(
      http.post(OVERVIEW_URLS.calls, () => HttpResponse.json({ calls: [] })),
      http.post(GET_CALL_URL, async () => {
        await new Promise((r) => setTimeout(r, 300));
        return HttpResponse.json(call());
      }),
    );
    renderApp(routes, "/calls");
    expect(await screen.findByText("Starting call")).toBeInTheDocument();
    expect(await screen.findByText("Ringing your extension 1001")).toBeInTheDocument();
  });

  it("shows Ending call while the platform releases the legs", async () => {
    startStored();
    server.use(
      http.post(OVERVIEW_URLS.calls, () => HttpResponse.json({ calls: [] })),
      http.post(GET_CALL_URL, () => HttpResponse.json(call({ state: "Terminating" }))),
    );
    renderApp(routes, "/calls");
    expect(await screen.findByText("Ending call…")).toBeInTheDocument();
    expect(screen.getByText("Releasing every leg.")).toBeInTheDocument();
  });

  it.each([
    ["caller_no_answer", "Call ended — 1001 did not answer", "1002 was never dialled.", "Disconnected"],
    ["destination unreachable", "Call ended — 1002 could not be reached", "1001 was released.", "Connected"],
    ["normal clearing", "Call ended — normal clearing", "1001 to 1002.", "Disconnected"],
    ["mystery reason", "Call ended — mystery reason", "1001 to 1002.", "Disconnected"],
    ["hangup by PJSIP/e_0123456789abcdef0123456789abcdef", "Call ended — Not shown", "1001 to 1002.", "Disconnected"],
  ])("names %j for an ending, never an engine identifier", async (reason, heading, detail, caller) => {
    startStored();
    server.use(
      http.post(OVERVIEW_URLS.calls, () => HttpResponse.json({ calls: [] })),
      http.post(GET_CALL_URL, () =>
        HttpResponse.json(
          call({
            state: "Terminated",
            endedAt: "2026-11-02T20:00:12Z",
            terminationReason: reason,
            participants: [
              { participantId: "p1", role: "CALLER", state: caller },
              { participantId: "p2", role: "AGENT", state: "Disconnected" },
            ],
          }),
        ),
      ),
    );
    renderApp(routes, "/calls");
    expect(await screen.findByText(heading)).toBeInTheDocument();
    expect(screen.getByText(detail)).toBeInTheDocument();
  });

  it("says Reconnecting on a failed poll and keeps the last state", async () => {
    startStored();
    server.use(
      http.post(OVERVIEW_URLS.calls, () => HttpResponse.json({ calls: [] })),
      http.post(GET_CALL_URL, () => HttpResponse.json({ code: "internal", message: "boom" }, { status: 500 })),
    );
    renderApp(routes, "/calls");
    expect(await screen.findByText("Reconnecting… the last known state is shown.")).toBeInTheDocument();
    expect(within(bar()).getByText("Starting call")).toBeInTheDocument();
  });

  it("shows a failed hang-up in the bar and keeps the call", async () => {
    startStored();
    server.use(
      http.post(OVERVIEW_URLS.calls, () => HttpResponse.json({ calls: [] })),
      http.post(GET_CALL_URL, () => HttpResponse.json(call())),
      http.post(HANGUP_CALL_URL, () => HttpResponse.json({ code: "permission_denied", message: "x" }, { status: 403 })),
    );
    const user = userEvent.setup();
    renderApp(routes, "/calls");
    await screen.findByText("Ringing your extension 1001");
    await user.click(within(bar()).getByRole("button", { name: "Cancel call to 1002" }));
    expect(await within(bar()).findByText("You do not have permission to do this.")).toBeInTheDocument();
    expect(within(bar()).getByText("Ringing your extension 1001")).toBeInTheDocument();
  });

  it("forgets a stored call the server does not know", async () => {
    startStored();
    server.use(
      http.post(OVERVIEW_URLS.calls, () => HttpResponse.json({ calls: [] })),
      http.post(GET_CALL_URL, () => HttpResponse.json({ code: "not_found", message: "gone" }, { status: 404 })),
    );
    renderApp(routes, "/calls");
    await screen.findByText("No calls yet");
    expect(screen.queryByRole("region", { name: "Current call" })).not.toBeInTheDocument();
    expect(sessionStorage.getItem(KEY)).toBeNull();
  });

  it("ignores a corrupt stored session", async () => {
    sessionStorage.setItem(KEY, "{not json");
    server.use(http.post(OVERVIEW_URLS.calls, () => HttpResponse.json({ calls: [] })));
    renderApp(routes, "/calls");
    await screen.findByText("No calls yet");
    expect(screen.queryByRole("region", { name: "Current call" })).not.toBeInTheDocument();
  });

  it("drops the stored session when the person switches Tenant", async () => {
    startStored();
    server.use(
      http.post(OVERVIEW_URLS.calls, () => HttpResponse.json({ calls: [] })),
      http.post(GET_CALL_URL, () => HttpResponse.json(call())),
      http.post("**/atsapbx.v1.AuthService/RefreshSession", () =>
        HttpResponse.json({
          access: { token: "t", tenantId: tenantB.tenantId, principalId: tenantB.principalId, expiresAt: "2030-01-01T00:00:00Z" },
          memberships: [tenantA, tenantB],
        }),
      ),
    );
    const { controller } = renderApp(routes, "/calls");
    await screen.findByText("Ringing your extension 1001");
    await act(async () => {
      await controller.chooseTenant(tenantB.tenantId);
    });
    await waitFor(() => expect(screen.queryByRole("region", { name: "Current call" })).not.toBeInTheDocument());
  });

  it.each([
    "{}",
    '{"callId":"c9"}',
    '{"callId":"c9","destination":"1002"}',
    '{"callId":"c9","destination":"1002","from":{}}',
    '{"callId":"c9","destination":"1002","from":{"id":"e","number":"1001"}}',
  ])("ignores a stored session missing fields: %s", async (raw) => {
    sessionStorage.setItem(KEY, raw);
    server.use(http.post(OVERVIEW_URLS.calls, () => HttpResponse.json({ calls: [] })));
    renderApp(routes, "/calls");
    await screen.findByText("No calls yet");
    expect(screen.queryByRole("region", { name: "Current call" })).not.toBeInTheDocument();
  });

  it("ticks the connected duration while the call stays up", async () => {
    startStored();
    server.use(
      http.post(OVERVIEW_URLS.calls, () => HttpResponse.json({ calls: [] })),
      http.post(GET_CALL_URL, () =>
        HttpResponse.json(
          call({
            state: "Active",
            answeredAt: new Date(Date.now() - 5000).toISOString(),
            participants: [
              { participantId: "p1", role: "CALLER", state: "Connected" },
              { participantId: "p2", role: "AGENT", state: "Connected" },
            ],
          }),
        ),
      ),
    );
    renderApp(routes, "/calls");
    await screen.findByText("In call with 1002");
    await new Promise((r) => setTimeout(r, 1100));
    expect(within(bar()).getByText(/\d+:\d{2} · from extension 1001/)).toBeInTheDocument();
  });
});
