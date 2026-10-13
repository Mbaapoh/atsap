import { useState } from "react";
import { Alert } from "../../components/Alert";
import { DataTable, type Column } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { RowActions } from "../../components/RowActions";
import { usePermissions } from "../auth/permissions";
import { useExtensionDirectory } from "../extensions/adapter";
import { useInboundNumbers, type InboundRow } from "./adapter";
import { AssignNumberDialog, extensionLabel, RemoveNumberDialog } from "./InboundDialogs";

// InboundSection lists which public numbers (DIDs) ring which extension,
// named by number and display name (CONSOLE-DESIGN "Inbound numbers").
export function InboundSection() {
  const numbers = useInboundNumbers();
  const directory = useExtensionDirectory();
  const { canAdminister } = usePermissions();
  const [assigning, setAssigning] = useState(false);
  const [removing, setRemoving] = useState<InboundRow | null>(null);
  const extensions = directory.data ?? [];
  const nameOf = (id: string) => extensionLabel(extensions.find((e) => e.id === id));

  const columns: Column<InboundRow>[] = [
    { key: "number", header: "Number", render: (n) => n.number, numeric: true },
    { key: "extension", header: "Rings extension", render: (n) => (directory.data ? nameOf(n.extensionId) : "…") },
  ];

  return (
    <section aria-labelledby="inbound-title" className="overview-section">
      <div className="section-header">
        <h2 id="inbound-title">Inbound numbers</h2>
        {canAdminister ? (
          <button type="button" className="button button-primary" disabled={extensions.length === 0} onClick={() => setAssigning(true)}>
            Assign number
          </button>
        ) : null}
      </div>
      {canAdminister && directory.data && extensions.length === 0 ? <p className="field-hint">Add an extension first: every number rings one.</p> : null}
      <AssignNumberDialog open={assigning} extensions={extensions} onClose={() => setAssigning(false)} />
      <RemoveNumberDialog target={removing} extension={removing ? nameOf(removing.extensionId) : ""} onClose={() => setRemoving(null)} />
      {numbers.isPending ? <p role="status">Loading inbound numbers…</p> : null}
      {numbers.isError ? (
        <Alert tone="danger" title="Inbound numbers could not be loaded">
          <p>{numbers.error.kind === "forbidden" ? "Your role cannot see this tenant's inbound numbers." : numbers.error.message}</p>
        </Alert>
      ) : null}
      {numbers.data ? (
        <DataTable
          caption="Inbound numbers of this tenant"
          captionHidden
          columns={columns}
          rows={numbers.data}
          rowKey={(n) => n.id}
          rowActions={canAdminister ? (n) => <RowActions label={`number ${n.number}`} actions={[{ label: "Remove number…", onSelect: () => setRemoving(n), destructive: true }]} /> : undefined}
          empty={<EmptyState title="No inbound numbers yet">Assign a number your carrier delivers so callers can reach an extension.</EmptyState>}
        />
      ) : null}
    </section>
  );
}
