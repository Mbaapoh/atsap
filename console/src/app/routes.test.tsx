import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { describe, expect, it } from "vitest";
import { primaryNav } from "./nav";
import { routes } from "./routes";

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  render(<RouterProvider router={router} />);
  return router;
}

describe("Phase A route tree", () => {
  it.each([
    ["/sign-in", "Sign in"],
    ["/", "Overview"],
    ["/licence", "Licence"],
    ["/users", "Users"],
    ["/extensions", "Extensions"],
    ["/trunks", "Trunks & routes"],
    ["/calls", "Call history"],
    ["/account", "Account"],
  ])("%s renders its page heading", (path, title) => {
    renderAt(path);
    expect(screen.getByRole("heading", { level: 1, name: title })).toBeInTheDocument();
  });

  it("an unknown address says so, inside the shell", () => {
    renderAt("/queues");
    expect(screen.getByRole("heading", { level: 1, name: "Page not found" })).toBeInTheDocument();
    expect(screen.getByRole("navigation", { name: "Main" })).toBeInTheDocument();
  });

  it("sign-in stands outside the shell", () => {
    renderAt("/sign-in");
    expect(screen.queryByRole("navigation", { name: "Main" })).not.toBeInTheDocument();
  });

  it("navigation offers only the Phase A screens", () => {
    renderAt("/");
    const nav = screen.getByRole("navigation", { name: "Main" });
    const labels = Array.from(nav.querySelectorAll("a")).map((a) => a.textContent);
    expect(labels).toEqual([...primaryNav.map((n) => n.label), "Account"]);
    for (const dormant of ["IVR", "Queues", "Voicemail", "Recordings", "Agents", "Reports", "Google"]) {
      expect(nav).not.toHaveTextContent(dormant);
    }
  });

  it("moves focus to the new page heading and announces it after navigation", async () => {
    const user = userEvent.setup();
    renderAt("/");
    await user.click(screen.getByRole("link", { name: "Extensions" }));
    const heading = await screen.findByRole("heading", { level: 1, name: "Extensions" });
    expect(heading).toHaveFocus();
    expect(screen.getByRole("status")).toHaveTextContent("Extensions page");
    expect(screen.getByRole("link", { name: "Extensions" })).toHaveAttribute("aria-current", "page");
  });
});
