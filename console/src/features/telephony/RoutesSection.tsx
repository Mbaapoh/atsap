import { useState } from "react";
import { Alert } from "../../components/Alert";
import { DataTable, type Column } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { Pager } from "../../components/Pager";
import { RowActions } from "../../components/RowActions";
import { usePermissions } from "../auth/permissions";
import { useRoutes, useTrunks, type RouteRow } from "./adapter";
import { DeleteRouteDialog, RouteDialog } from "./RouteDialogs";
import { formatCost } from "./routing";

// RoutesSection lists outbound routes. There is no "Edit route": the API
// has no update, so a change is an explicit, confirmed replace
// (CONSOLE-DESIGN "Trunks and outbound routes").
export function RoutesSection() {
  const [page, setPage] = useState(0);
  const routes = useRoutes(page);
  const trunks = useTrunks(0);
  const { canAdminister } = usePermissions();
  const [dialog, setDialog] = useState<{ open: boolean; replacing: RouteRow | null }>({ open: false, replacing: null });
  const [deleting, setDeleting] = useState<RouteRow | null>(null);
  const trunkList = trunks.data ?? [];
  const trunkName = (id: string) => trunkList.find((t) => t.id === id)?.name ?? "a trunk not shown";

  const columns: Column<RouteRow>[] = [
    { key: "prefix", header: "Prefix", render: (r) => r.prefix, numeric: true },
    { key: "trunk", header: "Trunk", render: (r) => trunkName(r.trunkId) },
    { key: "cost", header: "Stored cost / min", render: (r) => formatCost(r.costMicros), numeric: true },
    { key: "priority", header: "Priority", render: (r) => String(r.priority), numeric: true },
  ];

  return (
    <section aria-labelledby="routes-title" className="overview-section">
      <div className="section-header">
        <h2 id="routes-title">Outbound routes</h2>
        {canAdminister ? (
          <button type="button" className="button button-primary" disabled={trunkList.length === 0} onClick={() => setDialog({ open: true, replacing: null })}>
            Add route
          </button>
        ) : null}
      </div>
      {canAdminister && trunks.data && trunkList.length === 0 ? <p className="field-hint">Add a trunk first: every route sends calls to one.</p> : null}
      <RouteDialog open={dialog.open} replacing={dialog.replacing} routes={routes.data ?? []} trunks={trunkList} onClose={() => setDialog({ open: false, replacing: null })} />
      <DeleteRouteDialog route={deleting} trunkName={deleting ? trunkName(deleting.trunkId) : ""} onClose={() => setDeleting(null)} />
      {routes.isPending ? <p role="status">Loading routes…</p> : null}
      {routes.isError ? (
        <Alert tone="danger" title="Routes could not be loaded">
          <p>{routes.error.kind === "forbidden" ? "Your role cannot see this tenant's routes." : routes.error.message}</p>
        </Alert>
      ) : null}
      {routes.data ? (
        <>
          <DataTable
            caption="Outbound routes of this tenant"
            captionHidden
            columns={columns}
            rows={routes.data}
            rowKey={(r) => r.id}
            rowActions={
              canAdminister
                ? (r) => (
                    <RowActions
                      label={`route ${r.prefix}`}
                      actions={[
                        { label: "Replace…", onSelect: () => setDialog({ open: true, replacing: r }) },
                        { label: "Delete route…", onSelect: () => setDeleting(r), destructive: true },
                      ]}
                    />
                  )
                : undefined
            }
            empty={<EmptyState title="No outbound routes yet">Add a route so dialled numbers know which trunk to use.</EmptyState>}
          />
          <Pager page={page} rowCount={routes.data.length} onPage={setPage} label="Routes pages" />
        </>
      ) : null}
    </section>
  );
}
