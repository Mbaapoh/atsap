import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { PAGE_SIZE, Pager } from "./Pager";
import { RowActions } from "./RowActions";

// console-admin-screens task 1.2.

function PagedHarness({ rows }: { rows: (page: number) => number }) {
  const [page, setPage] = useState(0);
  return <Pager page={page} rowCount={rows(page)} onPage={setPage} label="Extensions pages" />;
}

describe("Pager", () => {
  it("is absent when everything fits one page", () => {
    render(<PagedHarness rows={() => 3} />);
    expect(screen.queryByRole("navigation", { name: "Extensions pages" })).not.toBeInTheDocument();
  });

  it("offers Next only after a full page, and never a total", async () => {
    const user = userEvent.setup();
    render(<PagedHarness rows={(p) => (p === 0 ? PAGE_SIZE : 7)} />);
    const nav = screen.getByRole("navigation", { name: "Extensions pages" });
    expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Next" }));
    expect(nav).toHaveTextContent("Page 2");
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
    expect(nav.textContent).not.toMatch(/of \d|total/i);
    await user.click(screen.getByRole("button", { name: "Previous" }));
    expect(nav).toHaveTextContent("Page 1");
  });
});

describe("RowActions", () => {
  function setup() {
    const edit = vi.fn();
    const remove = vi.fn();
    render(
      <RowActions
        label="1001"
        actions={[
          { label: "Delete extension", onSelect: remove, destructive: true },
          { label: "Edit", onSelect: edit },
          { label: "Rotate secret", onSelect: vi.fn(), disabled: true },
        ]}
      />,
    );
    return { edit, remove };
  }

  it("names its row and lists destructive actions last", async () => {
    const user = userEvent.setup();
    setup();
    const button = screen.getByRole("button", { name: "Actions for 1001" });
    expect(button).toHaveAttribute("aria-expanded", "false");
    await user.click(button);
    const items = screen.getAllByRole("menuitem").map((i) => i.textContent);
    expect(items).toEqual(["Edit", "Rotate secret", "Delete extension"]);
    expect(screen.getByRole("menu", { name: "Actions for 1001" })).toBeInTheDocument();
  });

  it("works by keyboard: open on Arrow Down, move, choose, focus returns", async () => {
    const user = userEvent.setup();
    const { remove } = setup();
    screen.getByRole("button", { name: "Actions for 1001" }).focus();
    await user.keyboard("{ArrowDown}");
    expect(screen.getByRole("menuitem", { name: "Edit" })).toHaveFocus();
    await user.keyboard("{ArrowDown}");
    expect(screen.getByRole("menuitem", { name: "Delete extension" })).toHaveFocus(); // the disabled item is skipped
    await user.keyboard("{Enter}");
    expect(remove).toHaveBeenCalledOnce();
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Actions for 1001" })).toHaveFocus();
  });

  it("Escape closes and returns focus; a click elsewhere closes", async () => {
    const user = userEvent.setup();
    setup();
    const button = screen.getByRole("button", { name: "Actions for 1001" });
    await user.click(button);
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
    expect(button).toHaveFocus();
    await user.click(button);
    await user.click(document.body);
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });
});
