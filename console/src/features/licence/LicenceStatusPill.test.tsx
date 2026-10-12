import { render, screen } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { describe, expect, it } from "vitest";
import { LicenseCondition } from "../../api/gen/atsapbx/v1/licensing_pb";
import { LicenceStatusPill, licencePresentation } from "./LicenceStatusPill";

function renderPill(condition: LicenseCondition, channels?: number) {
  const router = createMemoryRouter([{ path: "/", element: <LicenceStatusPill condition={condition} channels={channels} /> }]);
  render(<RouterProvider router={router} />);
  return screen.getByRole("link");
}

describe("LicenceStatusPill", () => {
  it.each([
    [LicenseCondition.SETUP, "Setup", "pill-info"],
    [LicenseCondition.FREE_COMMUNITY, "Free Community", "pill-info"],
    [LicenseCondition.VALID, "Valid", "pill-success"],
    [LicenseCondition.CAPACITY_WARNING, "Capacity warning", "pill-warning"],
    [LicenseCondition.CAPACITY_REACHED, "Capacity reached", "pill-danger"],
    [LicenseCondition.ENTITLEMENT_UNVERIFIED, "Entitlement unverified", "pill-warning"],
    [LicenseCondition.DEGRADED, "Degraded", "pill-danger"],
  ])("shows the server's condition %s as %s, linked to the licence page", (condition, label, tone) => {
    const pill = renderPill(condition, 16);
    expect(pill).toHaveTextContent(label);
    expect(pill).toHaveClass(tone);
    expect(pill).toHaveAttribute("href", "/licence");
    expect(pill).toHaveAccessibleName(`Licence: ${label} · 16 channels. Open licence details`);
  });

  it("never shows channels when there are none (Setup)", () => {
    expect(renderPill(LicenseCondition.SETUP, 0)).toHaveAccessibleName("Licence: Setup. Open licence details");
  });

  it("gives every condition its own label, so no two states are conflated", () => {
    const conditions = Object.values(LicenseCondition).filter((v): v is LicenseCondition => typeof v === "number");
    const labels = conditions.map((c) => licencePresentation(c).label);
    expect(new Set(labels).size).toBe(conditions.length);
  });
});
