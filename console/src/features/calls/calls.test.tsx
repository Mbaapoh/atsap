import { act, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { routes } from "../../app/routes";
import { OVERVIEW_URLS, server } from "../../test/msw";
import { renderApp, tenantA } from "../../test/session";
import { localMidnight } from "./filters";

// console-admin-screens task 5.1: call history (HLD-18 A5).

const ENGINE = "e_0123456789abcdef0123456789abcdef";
const answered = {
  callId: "c1",
  direction: "INBOUND",
  state: "Terminated",
  sourceNumber: "+237600000001",
  destNumber: "1001",
  startedAt: "2026-10-12T09:30:00Z",
  answeredAt: "2026-10-12T09:30:05Z",
  durationSeconds: 61,
  terminationReason: "normal clearing",
};
const refused = { callId: "c2", direction: "OUTBOUND", state: "Terminated", sourceNumber: "1001", destNumber: "+44207", startedAt: "2026-10-12T10:00:00Z", terminationReason: "capacity verdict rejected the call" };
const live = { callId: "c3", direction: "INTERNAL", state: "Presenting", sourceNumber: "1001", destNumber: "1002", startedAt: "2026-10-12T11:00:00Z" };

function calls(list: object[], bodies?: unknown[]) {
  return http.post(OVERVIEW_URLS.calls, async ({ request }) => {
    bodies?.push(await request.json());
    return HttpResponse.json({ calls: list });
  });
}

describe("call history", () => {
  it("shows time, direction, numbers, the state verbatim, duration and termination reason", async () => {
    server.use(calls([answered, refused, live]));
    renderApp(routes, "/calls");
    const table = within(await screen.findByRole("table", { name: "Calls of this tenant" }));
    const a = table.getByRole("row", { name: /\+237600000001/ });
    for (const text of ["Inbound", "1001", "Terminated", "1:01", "normal clearing"]) expect(a).toHaveTextContent(text);
    const r = table.getByRole("row", { name: /\+44207/ });
    expect(r).toHaveTextContent("Outbound");
    expect(r).toHaveTextContent("capacity verdict rejected the call");
    expect(within(r).getAllByRole("cell")[5]).toHaveTextContent("—"); // never answered: no duration
    const l = table.getByRole("row", { name: /1002/ });
    expect(l).toHaveTextContent("Presenting");
    expect(document.body.textContent).not.toMatch(/Answered|Missed|Failed/);
    expect(document.body.textContent).not.toMatch(/c1|c2|c3/);
  });

  it("sends all three filters together, and keeps them in the address", async () => {
    const bodies: Record<string, unknown>[] = [];
    server.use(calls([answered], bodies));
    const user = userEvent.setup();
    const { router } = renderApp(routes, "/calls");
    await screen.findByRole("table", { name: "Calls of this tenant" });
    expect(bodies[0]).toEqual({ tenantId: tenantA.tenantId, limit: 50 });

    const form = within(screen.getByRole("form", { name: "Call filters" }));
    await user.type(form.getByLabelText("From"), "2026-10-01");
    await user.type(form.getByLabelText("To"), "2026-10-12");
    await user.selectOptions(form.getByLabelText("Direction"), "Inbound");
    await user.selectOptions(form.getByLabelText("State"), "Terminated");
    await user.click(form.getByRole("button", { name: "Apply filters" }));

    await screen.findByRole("button", { name: "Clear filters" });
    expect(router.state.location.search).toBe("?from=2026-10-01&to=2026-10-12&direction=INBOUND&state=Terminated");
    expect(bodies.at(-1)).toEqual({
      tenantId: tenantA.tenantId,
      limit: 50,
      direction: "INBOUND",
      state: "Terminated",
      startedFrom: timestampToJson(localMidnight("2026-10-01")),
      startedTo: timestampToJson(localMidnight("2026-10-13")),
    });
  });

  it("offers only the server's lifecycle names as states", async () => {
    server.use(calls([]));
    renderApp(routes, "/calls");
    const state = await screen.findByLabelText("State");
    expect(within(state).getAllByRole("option").map((o) => o.textContent)).toEqual(["Any", "Initiated", "Screening", "Routing", "Presenting", "Active", "Degraded", "Terminating", "Terminated"]);
  });

  it("opening a filtered address (a reload or shared link) applies it; Back returns to the previous filters", async () => {
    const bodies: Record<string, unknown>[] = [];
    server.use(calls([], bodies));
    const user = userEvent.setup();
    const { router } = renderApp(routes, "/calls?direction=OUTBOUND&state=Active");
    expect(await screen.findByText("No calls match these filters")).toBeInTheDocument();
    expect(bodies[0]).toMatchObject({ direction: "OUTBOUND", state: "Active" });
    expect(screen.getByLabelText("Direction")).toHaveValue("OUTBOUND");
    expect(screen.getByLabelText("State")).toHaveValue("Active");

    await user.selectOptions(screen.getByLabelText("State"), "Terminated");
    await user.click(screen.getByRole("button", { name: "Apply filters" }));
    await act(async () => {
      await router.navigate(-1);
    });
    expect(router.state.location.search).toBe("?direction=OUTBOUND&state=Active");
    expect(screen.getByLabelText("State")).toHaveValue("Active");
    expect(bodies.map((b) => b.state)).toEqual(["Active", "Terminated"]); // Back reuses what was already read
  });

  it("refuses an end date before the start date, sending nothing", async () => {
    const bodies: unknown[] = [];
    server.use(calls([], bodies));
    const user = userEvent.setup();
    const { router } = renderApp(routes, "/calls");
    await screen.findByText("No calls yet");
    await user.type(screen.getByLabelText("From"), "2026-10-12");
    await user.type(screen.getByLabelText("To"), "2026-10-01");
    await user.click(screen.getByRole("button", { name: "Apply filters" }));
    expect(screen.getByLabelText("To")).toHaveAccessibleDescription("The end date is before the start date.");
    expect(router.state.location.search).toBe("");
    expect(bodies.filter((b) => "startedFrom" in (b as object) || "startedTo" in (b as object))).toEqual([]);
  });

  it("clearing the filters empties the address", async () => {
    server.use(calls([]));
    const user = userEvent.setup();
    const { router } = renderApp(routes, "/calls?state=Terminated");
    await user.click(await screen.findByRole("button", { name: "Clear filters" }));
    expect(router.state.location.search).toBe("");
    expect(await screen.findByText("No calls yet")).toBeInTheDocument();
  });

  it("pages with the filters kept, and only after a full page", async () => {
    const bodies: Record<string, unknown>[] = [];
    const page = Array.from({ length: 50 }, (_, i) => ({ ...answered, callId: `k${i}` }));
    server.use(calls(page, bodies));
    const user = userEvent.setup();
    const { router } = renderApp(routes, "/calls?state=Terminated");
    await screen.findByRole("table", { name: "Calls of this tenant" });
    await user.click(screen.getByRole("button", { name: "Next" }));
    expect(router.state.location.search).toBe("?state=Terminated&page=2");
    expect(bodies.at(-1)).toMatchObject({ state: "Terminated", offset: 50 });
  });

  it("never shows engine identifiers, whatever the server sends", async () => {
    server.use(calls([{ ...answered, sourceNumber: `PJSIP/${ENGINE}`, terminationReason: `hangup by PJSIP/${ENGINE} (stasis-in)` }]));
    renderApp(routes, "/calls");
    const row = within(await screen.findByRole("table", { name: "Calls of this tenant" })).getAllByRole("row")[1]!;
    expect(row).toHaveTextContent("Not shown");
    expect(document.body.textContent).not.toMatch(/PJSIP|e_[0-9a-f]{32}|stasis/);
  });

  it("a refused read is console copy", async () => {
    server.use(http.post(OVERVIEW_URLS.calls, () => HttpResponse.json({ code: "permission_denied", message: "ps_endpoints" }, { status: 403 })));
    renderApp(routes, "/calls");
    expect(await screen.findByText("Your role cannot see this tenant's calls.")).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("ps_endpoints");
  });
});

// timestampToJson is how a whole-second Timestamp travels in the Connect
// JSON body (RFC 3339, no fraction).
function timestampToJson(d: Date): string {
  return d.toISOString().replace(".000Z", "Z");
}
