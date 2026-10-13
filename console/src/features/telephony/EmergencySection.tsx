import { useState } from "react";
import { Alert } from "../../components/Alert";
import { DataTable, type Column } from "../../components/DataTable";
import { RowActions } from "../../components/RowActions";
import { usePermissions } from "../auth/permissions";
import { useEmergencyProfile } from "./adapter";
import { AddEmergencyNumberDialog, CountryDialog, RemoveEmergencyNumberDialog } from "./EmergencyDialogs";

type Row = { number: string; builtIn: boolean };

// EmergencySection shows the tenant's effective emergency set — 112, its
// country's catalogue numbers, and its own additions — as the server
// renders it. Built-in numbers carry no actions: the console never offers
// to remove them (CONSOLE-DESIGN "Emergency").
export function EmergencySection() {
  const profile = useEmergencyProfile();
  const { canAdminister } = usePermissions();
  const [dialog, setDialog] = useState<"country" | "add" | null>(null);
  const [removing, setRemoving] = useState<string | null>(null);
  const p = profile.data;

  const columns: Column<Row>[] = [
    { key: "number", header: "Number", render: (n) => n.number, numeric: true },
    { key: "kind", header: "Source", render: (n) => (n.builtIn ? "Built in" : "Added by this tenant") },
  ];

  return (
    <section aria-labelledby="emergency-title" className="overview-section">
      <div className="section-header">
        <h2 id="emergency-title">Emergency numbers</h2>
        {canAdminister && p ? (
          <div className="section-actions">
            <button type="button" className="button" onClick={() => setDialog("country")}>
              {p.country ? "Change country" : "Set country"}
            </button>
            <button type="button" className="button button-primary" onClick={() => setDialog("add")}>
              Add number
            </button>
          </div>
        ) : null}
      </div>
      <CountryDialog open={dialog === "country"} current={p?.country ?? ""} onClose={() => setDialog(null)} />
      <AddEmergencyNumberDialog open={dialog === "add"} onClose={() => setDialog(null)} />
      <RemoveEmergencyNumberDialog number={removing} onClose={() => setRemoving(null)} />
      {profile.isPending ? <p role="status">Loading emergency numbers…</p> : null}
      {profile.isError ? (
        <Alert tone="danger" title="Emergency numbers could not be loaded">
          <p>{profile.error.kind === "forbidden" ? "Your role cannot see this tenant's emergency settings." : profile.error.message}</p>
        </Alert>
      ) : null}
      {p ? (
        <>
          {!p.country ? (
            <Alert tone="warning" title="No emergency country is set">
              <p>Only 112 is recognised as an emergency number. Set this tenant's country so its emergency numbers are recognised too, or add them yourself.</p>
            </Alert>
          ) : !p.inCatalogue ? (
            <Alert tone="warning" title={`No built-in emergency numbers for ${p.country}`}>
              <p>The platform does not list this country's numbers. Add the numbers its emergency services answer.</p>
            </Alert>
          ) : (
            <p>
              Country: <strong>{p.country}</strong>. Its emergency numbers are built in.
            </p>
          )}
          <DataTable
            caption="Effective emergency numbers of this tenant"
            captionHidden
            columns={columns}
            rows={p.numbers}
            rowKey={(n) => n.number}
            rowActions={
              canAdminister
                ? (n) => (n.builtIn ? null : <RowActions label={`emergency number ${n.number}`} actions={[{ label: "Remove…", onSelect: () => setRemoving(n.number), destructive: true }]} />)
                : undefined
            }
            empty={<p>No emergency numbers.</p>}
          />
          <p className="field-hint">Calls to these numbers skip the licence and calling-rule checks and may exceed a trunk's channel limit, but they still leave through an outbound route: make sure one matches them.</p>
        </>
      ) : null}
    </section>
  );
}
