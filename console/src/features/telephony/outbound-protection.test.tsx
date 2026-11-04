import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { routes } from "../../app/routes";
import { OUTBOUND_POLICY_URL, RESUME_OUTBOUND_URL, server, UPDATE_OUTBOUND_POLICY_URL } from "../../test/msw";
import { renderApp, signedIn, tenantA } from "../../test/session";

const policy = {
  dailySpendCapMicros: "0", monthlySpendCapMicros: "0", blockedPrefixes: [],
  anomalyCallThreshold: 20, anomalyWindowSeconds: 600,
  businessStartMinute: 480, businessEndMinute: 1080, suspended: false,
};

describe("outbound protection", () => {
  it("shows the guard and saves the administrator's settings", async () => {
    let body: unknown;
    server.use(
      http.post(OUTBOUND_POLICY_URL, () => HttpResponse.json({ policy })),
      http.post(UPDATE_OUTBOUND_POLICY_URL, async ({ request }) => {
        body = await request.json();
        return HttpResponse.json({ policy });
      }),
    );
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    expect(await screen.findByRole("heading", { name: "Outbound protection" })).toBeInTheDocument();
    await user.clear(await screen.findByLabelText("Daily spend cap (micros)"));
    await user.type(screen.getByLabelText("Daily spend cap (micros)"), "5000000");
    await user.type(screen.getByLabelText("Blocked prefixes"), "+1900, +239");
    await user.click(screen.getByRole("button", { name: "Save outbound protection" }));
    await waitFor(() => expect(body).toBeDefined());
    const sent = body as Record<string, unknown>;
    expect(sent.tenantId).toBe("11111111-1111-4111-8111-111111111111");
    expect(Number(sent.dailySpendCapMicros)).toBe(5_000_000);
    expect(sent.monthlySpendCapMicros ?? "0").toBe("0");
    expect(sent.blockedPrefixes).toEqual(["+1900", "+239"]);
    expect(await screen.findByText("Outbound protection saved.")).toBeInTheDocument();
  });

  it("explains a suspension and resumes it", async () => {
    let resumed = false;
    server.use(
      http.post(OUTBOUND_POLICY_URL, () => HttpResponse.json({
        policy: { ...policy, suspended: true, suspendedReason: "anomaly: high-volume international calling outside business hours" },
      })),
      http.post(RESUME_OUTBOUND_URL, () => {
        resumed = true;
        return HttpResponse.json({ policy });
      }),
    );
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    expect(await screen.findByText("Outbound calling is suspended")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Resume outbound calling" }));
    await waitFor(() => expect(resumed).toBe(true));
    expect(await screen.findByText("Outbound calling resumed.")).toBeInTheDocument();
  });
});

describe("outbound protection edges", () => {
  it("is read-only without administrator rights", async () => {
    server.use(http.post(OUTBOUND_POLICY_URL, () => HttpResponse.json({ policy })));
    renderApp(routes, "/trunks", signedIn({ memberships: [{ ...tenantA, roles: ["AGENT"] }] }));
    expect(await screen.findByLabelText("Daily spend cap (micros)")).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Save outbound protection" })).not.toBeInTheDocument();
  });

  it("reports a policy that cannot be loaded", async () => {
    server.use(http.post(OUTBOUND_POLICY_URL, () => HttpResponse.json({ code: "permission_denied", message: "no" }, { status: 403 })));
    renderApp(routes, "/trunks");
    expect(await screen.findByText("Your role cannot see this setting.")).toBeInTheDocument();
  });

  it("reports a failed save and a failed resume", async () => {
    server.use(
      http.post(OUTBOUND_POLICY_URL, () => HttpResponse.json({ policy: { ...policy, suspended: true, suspendedAt: "2026-11-04T00:00:00Z", suspendedReason: "test" } })),
      http.post(UPDATE_OUTBOUND_POLICY_URL, () => HttpResponse.json({ code: "internal", message: "no" }, { status: 500 })),
      http.post(RESUME_OUTBOUND_URL, () => HttpResponse.json({ code: "internal", message: "no" }, { status: 500 })),
    );
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    await user.click(await screen.findByRole("button", { name: "Save outbound protection" }));
    expect((await screen.findAllByRole("alert")).length).toBeGreaterThan(0);
    await user.click(screen.getByRole("button", { name: "Resume outbound calling" }));
    expect((await screen.findAllByRole("alert")).length).toBeGreaterThan(0);
  });
});

describe("outbound protection branches", () => {
  it("saves empty fields as their defaults and accepts midnight hours", async () => {
    let body: Record<string, unknown> | undefined;
    server.use(
      http.post(OUTBOUND_POLICY_URL, () => HttpResponse.json({ policy })),
      http.post(UPDATE_OUTBOUND_POLICY_URL, async ({ request }) => {
        body = (await request.json()) as Record<string, unknown>;
        return HttpResponse.json({ policy });
      }),
    );
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    for (const label of ["Daily spend cap (micros)", "Monthly spend cap (micros)", "Blocked prefixes", "Fraud calls before suspension", "Fraud window (seconds)"]) {
      await user.clear(await screen.findByLabelText(label));
    }
    await user.clear(screen.getByLabelText("Business hours start (UTC)"));
    await user.clear(screen.getByLabelText("Business hours end (UTC)"));
    await user.click(screen.getByRole("button", { name: "Save outbound protection" }));
    await waitFor(() => expect(body).toBeDefined());
    expect(Number(body?.anomalyWindowSeconds)).toBe(600);
    expect(body?.businessStartMinute ?? 0).toBe(480);

    await user.type(screen.getByLabelText("Business hours start (UTC)"), "00:00");
    await user.type(screen.getByLabelText("Business hours end (UTC)"), "00:00");
    await user.click(screen.getByRole("button", { name: "Save outbound protection" }));
    await waitFor(() => expect(body?.businessStartMinute ?? 0).toBe(0));
    expect(body?.businessEndMinute ?? 0).toBe(0);
  });

  it("explains a generic load failure and an empty suspension reason", async () => {
    server.use(http.post(OUTBOUND_POLICY_URL, () => HttpResponse.json({ code: "internal", message: "no" }, { status: 500 })));
    const first = renderApp(routes, "/trunks");
    expect(await screen.findByText("Something went wrong. Try again, and contact your administrator if it continues.")).toBeInTheDocument();
    first.unmount();

    server.use(http.post(OUTBOUND_POLICY_URL, () => HttpResponse.json({ policy: { ...policy, suspended: true, suspendedReason: "" } })));
    renderApp(routes, "/trunks");
    expect(await screen.findByText(/An administrator suspended outbound calling/)).toBeInTheDocument();
  });
});
