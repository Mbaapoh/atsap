import { useState } from "react";
import { Alert } from "../../components/Alert";
import { DataTable, type Column } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { Pager } from "../../components/Pager";
import { RowActions } from "../../components/RowActions";
import { usePermissions } from "../auth/permissions";
import { useTrunks, type TrunkRow } from "./adapter";
import { TrunkHealthCell } from "./TrunkHealthCell";
import { DeleteTrunkDialog, TrunkDialog } from "./TrunkDialogs";

const columns: Column<TrunkRow>[] = [
  { key: "name", header: "Name", render: (t) => t.name },
  { key: "host", header: "Host", render: (t) => t.host },
  { key: "port", header: "Port", render: (t) => (t.port ? String(t.port) : "5060"), numeric: true },
  { key: "limit", header: "Channel limit", render: (t) => String(t.channelLimit), numeric: true },
  { key: "priority", header: "Priority", render: (t) => String(t.priority), numeric: true },
  { key: "auth", header: "Authentication", render: (t) => (t.authUsername ? (t.hasSecret ? "Username and secret" : "Username only") : "By IP address") },
  { key: "health", header: "Observed health", render: (t) => <TrunkHealthCell health={t.health} reason={t.healthReason} observedAt={t.healthObservedAt} /> },
];

// TrunksSection lists the tenant's carriers with the health the platform
// has observed (never a form field), and lets an administrator add, edit
// and delete them.
export function TrunksSection() {
  const [page, setPage] = useState(0);
  const trunks = useTrunks(page);
  const { canAdminister } = usePermissions();
  const [dialog, setDialog] = useState<{ open: boolean; trunk: TrunkRow | null }>({ open: false, trunk: null });
  const [deleting, setDeleting] = useState<TrunkRow | null>(null);

  return (
    <section aria-labelledby="trunks-title" className="overview-section">
      <div className="section-header">
        <h2 id="trunks-title">Trunks</h2>
        {canAdminister ? (
          <button type="button" className="button button-primary" onClick={() => setDialog({ open: true, trunk: null })}>
            Add trunk
          </button>
        ) : null}
      </div>
      <TrunkDialog open={dialog.open} trunk={dialog.trunk} onClose={() => setDialog({ open: false, trunk: null })} />
      <DeleteTrunkDialog trunk={deleting} onClose={() => setDeleting(null)} />
      {trunks.isPending ? <p role="status">Loading trunks…</p> : null}
      {trunks.isError ? (
        <Alert tone="danger" title="Trunks could not be loaded">
          <p>{trunks.error.kind === "forbidden" ? "Your role cannot see this tenant's trunks." : trunks.error.message}</p>
        </Alert>
      ) : null}
      {trunks.data ? (
        <>
          <DataTable
            caption="Trunks of this tenant"
            captionHidden
            columns={columns}
            rows={trunks.data}
            rowKey={(t) => t.id}
            rowActions={
              canAdminister
                ? (t) => (
                    <RowActions
                      label={`trunk ${t.name}`}
                      actions={[
                        { label: "Edit…", onSelect: () => setDialog({ open: true, trunk: t }) },
                        { label: "Delete trunk…", onSelect: () => setDeleting(t), destructive: true },
                      ]}
                    />
                  )
                : undefined
            }
            empty={<EmptyState title="No trunks yet">Add the SIP trunk your carrier gave you to make and receive outside calls.</EmptyState>}
          />
          <Pager page={page} rowCount={trunks.data.length} onPage={setPage} label="Trunks pages" />
        </>
      ) : null}
    </section>
  );
}
