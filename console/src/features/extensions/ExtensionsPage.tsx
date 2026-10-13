import { useState } from "react";
import { usePermissions } from "../auth/permissions";
import { AddExtensionDialog } from "./AddExtensionDialog";
import { CredentialsPanel } from "./CredentialsPanel";
import { Alert } from "../../components/Alert";
import { DataTable, type Column } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { PageHeader } from "../../components/PageHeader";
import { Pager } from "../../components/Pager";
import { StatusBadge } from "../../components/StatusBadge";
import { useExtensions, type Credentials, type ExtensionRow } from "./adapter";
import { deviceLabel, registrationBadge } from "./copy";

export const extensionColumns: Column<ExtensionRow>[] = [
  { key: "number", header: "Number", render: (e) => e.number, numeric: true },
  { key: "name", header: "Name", render: (e) => e.name },
  { key: "device", header: "Device", render: (e) => deviceLabel[e.device] },
  { key: "registration", header: "Registration", render: (e) => <StatusBadge {...registrationBadge[e.registration]} /> },
];

// ExtensionsPage lists the tenant's extensions with their observed
// registration (CONSOLE-DESIGN "Extensions"). No owner or assignment
// field until Phase B (D-65).
export function ExtensionsPage() {
  const [page, setPage] = useState(0);
  const extensions = useExtensions(page);
  const { canAdminister } = usePermissions();
  const [adding, setAdding] = useState(false);
  // credentials hold a just-issued secret until the person confirms
  // they saved it; nothing else ever holds it.
  const [credentials, setCredentials] = useState<Credentials | null>(null);

  return (
    <section className="page" aria-labelledby="page-title">
      <PageHeader
        title="Extensions"
        description="Numbers your phones register to. Registration is what the platform currently observes."
        action={
          canAdminister ? (
            <button type="button" className="button button-primary" onClick={() => setAdding(true)}>
              Add extension
            </button>
          ) : undefined
        }
      />
      <AddExtensionDialog open={adding} onClose={() => setAdding(false)} onCreated={setCredentials} />
      <CredentialsPanel
        credentials={credentials}
        title={`Extension ${credentials?.extension.number ?? ""} created`}
        onDone={() => setCredentials(null)}
      />
      {extensions.isPending ? <p role="status">Loading extensions…</p> : null}
      {extensions.isError ? (
        <Alert tone="danger" title="Extensions could not be loaded">
          <p>{extensions.error.kind === "forbidden" ? "Your role cannot see this tenant's extensions." : extensions.error.message}</p>
        </Alert>
      ) : null}
      {extensions.data ? (
        <>
          <DataTable
            caption="Extensions of this tenant"
            captionHidden
            columns={extensionColumns}
            rows={extensions.data}
            rowKey={(e) => e.id}
            empty={<EmptyState title="No extensions yet">Add an extension, then register a phone to it with the details shown.</EmptyState>}
          />
          <Pager page={page} rowCount={extensions.data.length} onPage={setPage} label="Extensions pages" />
        </>
      ) : null}
    </section>
  );
}
