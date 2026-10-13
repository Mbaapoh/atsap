import { screen, within } from "@testing-library/react";
import { http, HttpResponse, type JsonBodyType } from "msw";
import { describe, expect, it } from "vitest";
import { routes } from "../../app/routes";
import { LICENCE_STATUS_URL, OVERVIEW_URLS, server, validLicence } from "../../test/msw";
import { renderApp, tenantA } from "../../test/session";
import { COMPLETED_WINDOW, RECENT_CALLS } from "./adapter";

// console-foundation task 3.1: the API-backed setup checklist and
// summaries. Nothing on the page is estimated: each step is done, to do,
// or "could not check", by a rule the page states.

const at = "2026-10-12T09:30:00Z";

async function item(title: string) {
  const heading = await screen.findByText(title, { selector: ".checklist-title" });
  const li = heading.closest("li");
  if (!li) throw new Error(`no checklist item ${title}`);
  return li;
}

async function expectState(title: string, state: string) {
  await expect.poll(async () => (await item(title)).textContent).toContain(state);
}

describe("setup checklist", () => {
  it("a new tenant: licence in force, everything else still to do", async () => {
    renderApp(routes, "/");
    await expectState("Apply a licence", "Done");
    for (const t of ["Add a user", "Add an extension", "Connect a trunk and a route", "Complete a call"]) {
      await expectState(t, "To do");
    }
    expect(await screen.findByText("No calls yet")).toBeInTheDocument();
  });

  it("every step done, each from its own API fact", async () => {
    const bodies: Record<string, unknown[]> = {};
    const record = (name: string, response: JsonBodyType) =>
      http.post(OVERVIEW_URLS[name as keyof typeof OVERVIEW_URLS], async ({ request }) => {
        (bodies[name] ??= []).push(await request.json());
        return HttpResponse.json(response);
      });
    server.use(
      record("principals", { principals: [{}, {}] }),
      record("extensions", { extensions: [{ extensionId: "e" }] }),
      record("trunks", { trunks: [{ trunkId: "t1", name: "Carrier A", health: "TRUNK_HEALTH_DEGRADED" }] }),
      record("routes", { routes: [{ routeId: "r" }] }),
      record("calls", {
        calls: [{ callId: "c1", direction: "OUTBOUND", state: "Terminated", sourceNumber: "1001", destNumber: "+15550100", startedAt: at, answeredAt: at, durationSeconds: 125 }],
      }),
    );
    renderApp(routes, "/");

    for (const t of ["Apply a licence", "Add a user", "Add an extension", "Connect a trunk and a route", "Complete a call"]) {
      await expectState(t, "Done");
    }
    // Every read names the session's Tenant; the checklist asks for ended
    // calls only, the recent list for the latest few of any state.
    for (const name of Object.keys(bodies)) {
      for (const body of bodies[name]!) expect(body).toMatchObject({ tenantId: tenantA.tenantId });
    }
    expect(bodies.calls).toContainEqual({ tenantId: tenantA.tenantId, state: "Terminated", limit: COMPLETED_WINDOW });
    expect(bodies.calls).toContainEqual({ tenantId: tenantA.tenantId, limit: RECENT_CALLS });

    const calls = within(await screen.findByRole("region", { name: "Recent calls" }));
    expect(calls.getByText("Outbound")).toBeInTheDocument();
    expect(calls.getByText("Terminated")).toBeInTheDocument();
    expect(calls.getByText("2:05")).toBeInTheDocument();
    const trunks = within(await screen.findByRole("region", { name: "Trunk health" }));
    expect(trunks.getByText("Carrier A")).toBeInTheDocument();
    expect(trunks.getByText("Degraded")).toBeInTheDocument();
  });

  it("a call that ended without being answered does not complete the step", async () => {
    server.use(
      http.post(OVERVIEW_URLS.calls, () =>
        HttpResponse.json({ calls: [{ callId: "c1", direction: "INBOUND", state: "Terminated", startedAt: at, durationSeconds: 0 }] }),
      ),
    );
    renderApp(routes, "/");
    await expectState("Complete a call", "To do");
  });

  it("a trunk without a route is not a connected trunk", async () => {
    server.use(http.post(OVERVIEW_URLS.trunks, () => HttpResponse.json({ trunks: [{ trunkId: "t", name: "Carrier A", health: "TRUNK_HEALTH_HEALTHY" }] })));
    renderApp(routes, "/");
    await expectState("Connect a trunk and a route", "To do");
  });

  it("in Setup the licence step is to do", async () => {
    server.use(http.post(LICENCE_STATUS_URL, () => HttpResponse.json({ status: { ...validLicence, condition: "LICENSE_CONDITION_SETUP", channels: 0 } })));
    renderApp(routes, "/");
    await expectState("Apply a licence", "To do");
    expect(await screen.findByText("No channels licensed")).toBeInTheDocument();
  });

  it("a fact the role cannot read is reported as unchecked, never as done", async () => {
    server.use(http.post(OVERVIEW_URLS.principals, () => HttpResponse.json({ code: "permission_denied", message: "x" }, { status: 403 })));
    renderApp(routes, "/");
    await expectState("Add a user", "Could not check");
    expect((await item("Add a user")).textContent).toContain("Your role cannot see this, so it was not checked.");
  });

  it("a fact that fails to load is reported as unchecked", async () => {
    server.use(http.post(OVERVIEW_URLS.routes, () => HttpResponse.json({ code: "unavailable", message: "x" }, { status: 503 })));
    renderApp(routes, "/");
    await expectState("Connect a trunk and a route", "Could not check");
    expect((await item("Connect a trunk and a route")).textContent).toContain("This could not be checked just now.");
  });

  it("states each step's rule", async () => {
    renderApp(routes, "/");
    expect((await item("Complete a call")).textContent).toContain("Done when one of the latest ended calls was answered before it ended.");
  });
});

describe("summaries", () => {
  it("shows the server's licence condition and channels in use", async () => {
    server.use(http.post(LICENCE_STATUS_URL, () => HttpResponse.json({ status: { ...validLicence, condition: "LICENSE_CONDITION_CAPACITY_WARNING", channelsInUse: 13 } })));
    renderApp(routes, "/");
    const card = within(await screen.findByRole("region", { name: "Licence" }));
    expect(card.getByText(/Capacity warning/)).toBeInTheDocument();
    expect(card.getByText("13 of 16 channels in use")).toBeInTheDocument();
  });

  it("shows a call state exactly as the server reports it", async () => {
    server.use(http.post(OVERVIEW_URLS.calls, () => HttpResponse.json({ calls: [{ callId: "c", direction: "INBOUND", state: "Presenting", startedAt: at }] })));
    renderApp(routes, "/");
    const calls = within(await screen.findByRole("region", { name: "Recent calls" }));
    expect(await calls.findByText("Presenting")).toBeInTheDocument();
    expect(calls.queryByText(/Answered|Missed|Failed/)).not.toBeInTheDocument();
  });

  it("invents no metrics: no CPU, memory, uptime or health score", async () => {
    renderApp(routes, "/");
    await screen.findByText("No calls yet");
    expect(document.body.textContent).not.toMatch(/CPU|memory|uptime|health score|%/i);
  });

  it("hides the trunk summary when there are no trunks", async () => {
    renderApp(routes, "/");
    await screen.findByText("No calls yet");
    expect(screen.queryByRole("region", { name: "Trunk health" })).not.toBeInTheDocument();
  });
});
