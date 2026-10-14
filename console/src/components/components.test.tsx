import { act, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { Alert } from "./Alert";
import { ConfirmDialog } from "./ConfirmDialog";
import { DataTable, type Column } from "./DataTable";
import { EmptyState } from "./EmptyState";
import { StatusBadge } from "./StatusBadge";
import { TOAST_MS, ToastProvider, useToast } from "./Toast";

describe("StatusBadge", () => {
  it("pairs the colour with words and an icon hidden from assistive technology", () => {
    render(<StatusBadge tone="success" label="Registered" />);
    const badge = screen.getByText("Registered").parentElement!;
    expect(badge).toHaveClass("badge-success");
    expect(badge.querySelector("svg")).toHaveAttribute("aria-hidden", "true");
  });
});

describe("Alert", () => {
  it("interrupts for danger and warning, waits for info and success", () => {
    const { rerender } = render(<Alert tone="danger" title="Licence not applied" />);
    expect(screen.getByRole("alert")).toHaveTextContent("Licence not applied");
    rerender(<Alert tone="info" title="Licence applied" />);
    expect(screen.getByRole("status")).toHaveTextContent("Licence applied");
  });

  it("takes focus when asked, as an error summary must", () => {
    render(<Alert tone="danger" title="The extension could not be saved" focusOnMount />);
    expect(screen.getByRole("alert")).toHaveFocus();
  });
});

interface Ext {
  id: string;
  number: string;
  name: string;
}
const cols: Column<Ext>[] = [
  { key: "number", header: "Number", render: (r) => r.number, numeric: true },
  { key: "name", header: "Name", render: (r) => r.name },
];

describe("DataTable", () => {
  it("names its table and its keyboard-scrollable region, and keeps every column", () => {
    render(
      <DataTable caption="Extensions" columns={cols} rowKey={(r) => r.id}
        rows={[{ id: "1", number: "1001", name: "Reception" }]}
        rowActions={(r) => <button type="button">Edit {r.number}</button>} />,
    );
    const region = screen.getByRole("region", { name: "Extensions" });
    expect(region).toHaveAttribute("tabindex", "0");
    const table = within(region).getByRole("table", { name: "Extensions" });
    expect(within(table).getAllByRole("columnheader").map((h) => h.textContent)).toEqual(["Number", "Name", "Actions"]);
    expect(within(table).getByRole("cell", { name: "1001" })).toHaveClass("numeric-cell");
    expect(within(table).getByRole("button", { name: "Edit 1001" })).toBeInTheDocument();
  });

  it("shows the empty state instead of an empty table", () => {
    render(<DataTable caption="Extensions" columns={cols} rowKey={(r) => r.id} rows={[]}
      empty={<EmptyState title="No extensions yet">Add one to register a phone.</EmptyState>} />);
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    expect(screen.getByText("No extensions yet")).toBeInTheDocument();
  });
});

function DeleteRoute({ onConfirm, busy = false }: { onConfirm: () => void; busy?: boolean }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>Delete route +44</button>
      <ConfirmDialog open={open} title="Delete route +44?" confirmLabel="Delete route" destructive busy={busy}
        onConfirm={() => { onConfirm(); setOpen(false); }} onCancel={() => setOpen(false)}>
        Calls to +44 numbers will use the next matching route.
      </ConfirmDialog>
    </>
  );
}

describe("ConfirmDialog", () => {
  it("names the resource, starts on Cancel, and keeps Tab inside", async () => {
    const user = userEvent.setup();
    render(<DeleteRoute onConfirm={() => {}} />);
    await user.click(screen.getByRole("button", { name: "Delete route +44" }));

    const dialog = screen.getByRole("dialog", { name: "Delete route +44?" });
    expect(dialog).toHaveAttribute("aria-modal", "true");
    expect(dialog).toHaveAccessibleDescription("Calls to +44 numbers will use the next matching route.");
    const cancel = within(dialog).getByRole("button", { name: "Cancel" });
    const confirm = within(dialog).getByRole("button", { name: "Delete route" });
    expect(cancel).toHaveFocus();

    await user.tab();
    expect(confirm).toHaveFocus();
    await user.tab();
    expect(cancel).toHaveFocus();
    await user.tab({ shift: true });
    expect(confirm).toHaveFocus();
  });

  it("cancels on Escape and returns focus to what opened it", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn<() => void>();
    render(<DeleteRoute onConfirm={onConfirm} />);
    const opener = screen.getByRole("button", { name: "Delete route +44" });
    await user.click(opener);
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(onConfirm).not.toHaveBeenCalled();
    expect(opener).toHaveFocus();
  });

  it("confirms with its explicit verb", async () => {
    const user = userEvent.setup();
    const onConfirm = vi.fn<() => void>();
    render(<DeleteRoute onConfirm={onConfirm} />);
    await user.click(screen.getByRole("button", { name: "Delete route +44" }));
    await user.click(screen.getByRole("button", { name: "Delete route" }));
    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("cannot be sent twice while busy", async () => {
    const user = userEvent.setup();
    render(<DeleteRoute onConfirm={() => {}} busy />);
    await user.click(screen.getByRole("button", { name: "Delete route +44" }));
    expect(screen.getByRole("button", { name: "Delete route" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeDisabled();
  });
});

function Announcer() {
  const toast = useToast();
  return <button type="button" onClick={() => toast.show("Extension 1001 created")}>Create</button>;
}

describe("Toast", () => {
  it("announces politely, can be dismissed, and leaves by itself", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<ToastProvider><Announcer /></ToastProvider>);
    const region = screen.getByRole("status", { name: "Notifications" });
    expect(region).toHaveAttribute("aria-live", "polite");

    await user.click(screen.getByRole("button", { name: "Create" }));
    expect(region).toHaveTextContent("Extension 1001 created");
    await user.click(screen.getByRole("button", { name: "Dismiss notification" }));
    expect(region).not.toHaveTextContent("Extension 1001 created");

    await user.click(screen.getByRole("button", { name: "Create" }));
    act(() => { vi.advanceTimersByTime(TOAST_MS + 10); });
    expect(region).not.toHaveTextContent("Extension 1001 created");
    vi.useRealTimers();
  });

  it("stays while someone is reading it", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(<ToastProvider><Announcer /></ToastProvider>);
    await user.click(screen.getByRole("button", { name: "Create" }));
    act(() => { screen.getByRole("button", { name: "Dismiss notification" }).focus(); });
    act(() => { vi.advanceTimersByTime(TOAST_MS * 2); });
    expect(screen.getByRole("status", { name: "Notifications" })).toHaveTextContent("Extension 1001 created");
    vi.useRealTimers();
  });
});
