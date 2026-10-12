import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { primaryNav } from "./nav";
import { routes } from "./routes";
import type { SessionState } from "../features/auth/session";
import { renderApp } from "../test/session";
import { initialSession } from "../features/auth/session";

function renderAt(path: string, state?: SessionState) {
  return renderApp(routes, path, state).router;
}

describe("Phase A route tree", () => {
  it.each([
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
    renderAt("/sign-in", { ...initialSession, status: "signed-out" });
    expect(screen.getByRole("heading", { level: 1, name: "Sign in" })).toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Main" })).not.toBeInTheDocument();
  });

  it("navigation offers only the Phase A workflow screens; Licence lives in the top bar", () => {
    renderAt("/");
    const nav = screen.getByRole("navigation", { name: "Main" });
    const labels = Array.from(nav.querySelectorAll("a")).map((a) => a.textContent);
    expect(labels).toEqual([...primaryNav.map((n) => n.label), "Account"]);
    expect(labels).toEqual(["Overview", "Users", "Extensions", "Trunks & routes", "Call history", "Account"]);
    for (const dormant of ["Licence", "IVR", "Queues", "Voicemail", "Recordings", "Agents", "Reports", "Google"]) {
      expect(nav).not.toHaveTextContent(dormant);
    }
  });

  it("moves focus to the new page heading and announces it after navigation", async () => {
    const user = userEvent.setup();
    renderAt("/");
    await user.click(screen.getByRole("link", { name: "Extensions" }));
    const heading = await screen.findByRole("heading", { level: 1, name: "Extensions" });
    await waitFor(() => expect(heading).toHaveFocus());
    expect(screen.getByText("Extensions page")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Extensions" })).toHaveAttribute("aria-current", "page");
  });

  it("the small-screen drawer traps focus, closes on Escape and returns focus to the menu button", async () => {
    const user = userEvent.setup();
    renderAt("/");
    const menu = screen.getByRole("button", { name: "Open navigation" });
    expect(menu).toHaveAttribute("aria-expanded", "false");
    await user.click(menu);

    const drawer = screen.getByRole("dialog", { name: "AtsaPBX" });
    expect(menu).toHaveAttribute("aria-expanded", "true");
    const close = within(drawer).getByRole("button", { name: "Close navigation" });
    expect(close).toHaveFocus();
    const links = within(drawer).getAllByRole("link");
    links[links.length - 1]!.focus();
    await user.tab();
    expect(close).toHaveFocus();

    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(menu).toHaveFocus();
  });

  it("choosing a page in the drawer closes it and lands on the page heading", async () => {
    const user = userEvent.setup();
    renderAt("/");
    await user.click(screen.getByRole("button", { name: "Open navigation" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("link", { name: "Call history" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    const heading = await screen.findByRole("heading", { level: 1, name: "Call history" });
    await waitFor(() => expect(heading).toHaveFocus());
  });
});
