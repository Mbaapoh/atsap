import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { createMemoryRouter, Link, RouterProvider } from "react-router";
import { describe, expect, it, vi } from "vitest";
import { OneTimeSecret } from "./OneTimeSecret";

// console-admin-screens task 1.3.

const SECRET = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP";
const USERNAME = "e_0123456789abcdef0123456789abcdef";

function Page() {
  const [open, setOpen] = useState(true);
  return (
    <>
      <Link to="/elsewhere">Elsewhere</Link>
      {open ? null : <p>Panel closed</p>}
      <OneTimeSecret
        open={open}
        title="Extension 1001 created"
        intro={<p>These details are shown once.</p>}
        fields={[
          { label: "SIP username", value: USERNAME, sipUsername: true },
          { label: "SIP password", value: SECRET },
        ]}
        onDone={() => setOpen(false)}
      />
    </>
  );
}

function renderPanel() {
  const router = createMemoryRouter(
    [
      { path: "/", element: <Page /> },
      { path: "/elsewhere", element: <h1>Elsewhere</h1> },
    ],
    { initialEntries: ["/"] },
  );
  const root = document.createElement("div");
  root.id = "root";
  document.body.appendChild(root);
  render(<RouterProvider router={router} />, { container: root });
  return router;
}

describe("OneTimeSecret", () => {
  it("shows the values, marks only the SIP username, and copies each", async () => {
    // After setup: user-event installs its own clipboard stub.
    const user = userEvent.setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    renderPanel();

    const dialog = screen.getByRole("dialog", { name: "Extension 1001 created" });
    expect(dialog).toHaveTextContent(SECRET);
    expect(dialog.querySelectorAll("[data-sip-username]")).toHaveLength(1);
    expect(dialog.querySelector("[data-sip-username]")).toHaveTextContent(USERNAME);
    await user.click(screen.getByRole("button", { name: "Copy SIP password" }));
    expect(writeText).toHaveBeenCalledWith(SECRET);
    expect(screen.getByRole("button", { name: "Copy SIP password" })).toHaveTextContent("Copied");
  });

  it("cannot be dismissed until the person confirms they saved the details", async () => {
    const user = userEvent.setup();
    renderPanel();
    await user.keyboard("{Escape}");
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Done" })).toBeDisabled();
    await user.click(screen.getByRole("checkbox", { name: "I have saved these details" }));
    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(screen.getByText("Panel closed")).toBeInTheDocument();
    expect(document.body.textContent).not.toContain(SECRET);
  });

  it("holds an in-app navigation until Done, then lets it continue", async () => {
    const router = renderPanel();
    await act(async () => {
      await router.navigate("/elsewhere");
    });
    expect(router.state.location.pathname).toBe("/");
    expect(screen.getByText("Save these details first")).toBeInTheDocument();

    const user = userEvent.setup();
    await user.click(screen.getByRole("checkbox", { name: "I have saved these details" }));
    expect(router.state.location.pathname).toBe("/"); // ticking alone does not leave
    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(await screen.findByRole("heading", { name: "Elsewhere" })).toBeInTheDocument();
  });

  it("asks before the browser leaves while open", () => {
    renderPanel();
    const event = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(true);
  });
});
