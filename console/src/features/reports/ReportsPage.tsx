import { useId, useState, type FormEvent } from "react";
import { toApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { DataTable, type Column } from "../../components/DataTable";
import { PageHeader } from "../../components/PageHeader";
import { Pager } from "../../components/Pager";
import { TextField } from "../../components/TextField";
import { useExtensionDirectory } from "../extensions/adapter";
import { useUsers } from "../users/adapter";
import { blankReportFilters, useCallRecords, useExportCallRecords, type CallRecord, type ReportFilters } from "./adapter";

function shortDate(d: Date | null): string {
  return d ? d.toLocaleString() : "—";
}

// ReportsPage is LLD-09's call-detail report: terminated calls only, with
// participant attribution and a CSV export. It never shows an engine
// endpoint.
export function ReportsPage() {
  const fromId = useId();
  const toId = useId();
  const directionId = useId();
  const extensionId = useId();
  const principalId = useId();
  const numberId = useId();
  const [filters, setFilters] = useState<ReportFilters>(blankReportFilters);
  const [applied, setApplied] = useState<ReportFilters>(blankReportFilters);
  const records = useCallRecords(applied);
  const extensions = useExtensionDirectory();
  const users = useUsers(0);
  const exporter = useExportCallRecords();
  const [failure, setFailure] = useState<string | null>(null);

  const extensionName = (id: string) => extensions.data?.find((e) => e.id === id)?.number ?? "";
  const personName = (id: string) => {
    const u = users.data?.find((p) => p.principalId === id);
    return u ? (u.name ? `${u.name} · ${u.email}` : u.email) : "";
  };

  const columns: Column<CallRecord>[] = [
    { key: "started", header: "Started", render: (r) => shortDate(r.startedAt) },
    { key: "direction", header: "Direction", render: (r) => r.direction },
    { key: "from", header: "From", render: (r) => r.from, numeric: true },
    { key: "to", header: "To", render: (r) => r.to, numeric: true },
    { key: "duration", header: "Duration", render: (r) => `${r.durationSeconds}s`, numeric: true },
    { key: "reason", header: "Ended", render: (r) => r.reason || "—" },
    {
      key: "people", header: "Participants", render: (r) =>
        r.participants.map((p) => {
          const ext = p.extensionId ? extensionName(p.extensionId) : "";
          const person = p.principalId ? personName(p.principalId) : "";
          const label = [ext, person].filter(Boolean).join(" · ");
          return label ? `${p.role}: ${label}` : p.role;
        }).join("; ") || "—",
    },
  ];

  function onFilter(ev: FormEvent) {
    ev.preventDefault();
    setApplied({ ...filters, page: 0 });
  }

  async function onExport() {
    setFailure(null);
    try {
      const blob = await exporter.mutateAsync(applied);
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "call-records.csv";
      a.click();
      URL.revokeObjectURL(url);
    } catch (err) {
      setFailure(toApiError(err).message);
    }
  }

  return (
    <section className="page" aria-labelledby="page-title">
      <PageHeader
        title="Reports"
        description="Terminated calls with duration, outcome and the people on them. Exports are audited."
        action={<button type="button" className="button" onClick={() => void onExport()} disabled={exporter.isPending}>Export CSV</button>}
      />
      <form aria-label="Report filters" className="filter-bar" onSubmit={onFilter}>
        <div className="field">
          <label className="field-label" htmlFor={fromId}>From</label>
          <input id={fromId} className="input" type="date" value={filters.from} onChange={(e) => setFilters({ ...filters, from: e.target.value })} />
        </div>
        <div className="field">
          <label className="field-label" htmlFor={toId}>To</label>
          <input id={toId} className="input" type="date" value={filters.to} onChange={(e) => setFilters({ ...filters, to: e.target.value })} />
        </div>
        <div className="field">
          <label className="field-label" htmlFor={directionId}>Direction</label>
          <select id={directionId} className="input" value={filters.direction} onChange={(e) => setFilters({ ...filters, direction: e.target.value })}>
            <option value="">Any</option>
            <option value="INBOUND">Inbound</option>
            <option value="OUTBOUND">Outbound</option>
            <option value="INTERNAL">Internal</option>
          </select>
        </div>
        <div className="field">
          <label className="field-label" htmlFor={extensionId}>Extension</label>
          <select id={extensionId} className="input" value={filters.extensionId} onChange={(e) => setFilters({ ...filters, extensionId: e.target.value })}>
            <option value="">Any</option>
            {(extensions.data ?? []).map((e) => <option key={e.id} value={e.id}>{e.number}</option>)}
          </select>
        </div>
        <div className="field">
          <label className="field-label" htmlFor={principalId}>Person</label>
          <select id={principalId} className="input" value={filters.principalId} onChange={(e) => setFilters({ ...filters, principalId: e.target.value })}>
            <option value="">Any</option>
            {(users.data ?? []).map((u) => <option key={u.principalId} value={u.principalId}>{u.name || u.email}</option>)}
          </select>
        </div>
        <div className="field">
          <label className="field-label" htmlFor={numberId}>Number</label>
          <TextField label="Number" value={filters.number} onChange={(e) => setFilters({ ...filters, number: e.target.value })} />
        </div>
        <button type="submit" className="button button-primary">Apply filters</button>
      </form>
      {failure ? <Alert tone="danger" title={failure} /> : null}
      {records.isPending ? <p role="status">Loading call records…</p> : null}
      {records.isError ? <Alert tone="danger" title="Call records could not be loaded"><p>{records.error.message}</p></Alert> : null}
      {records.data ? (
        <>
          <DataTable caption="Call records of this tenant" captionHidden columns={columns} rows={records.data} rowKey={(r) => r.id} empty={<p>No terminated calls match these filters.</p>} />
          <Pager page={applied.page} rowCount={records.data.length} onPage={(page) => setApplied({ ...applied, page })} label="Report pages" />
        </>
      ) : null}
    </section>
  );
}
