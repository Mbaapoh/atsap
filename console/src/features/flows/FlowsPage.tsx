import { useId, useState, type FormEvent } from "react";
import { toApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { DataTable, type Column } from "../../components/DataTable";
import { Modal } from "../../components/Modal";
import { PageHeader } from "../../components/PageHeader";
import { TextField } from "../../components/TextField";
import { useToast } from "../../components/Toast";
import { usePermissions } from "../auth/permissions";
import { useExtensionDirectory } from "../extensions/adapter";
import { useFlowChanges, useFlows, useFlowVersions, useHolidayChanges, useHolidays, type Flow, type FlowConfig, type FlowInput } from "./adapter";

function clock(minute: number): string {
  return `${String(Math.floor(minute / 60)).padStart(2, "0")}:${String(minute % 60).padStart(2, "0")}`;
}
function minutes(v: string): number {
  const [h, m] = v.split(":").map(Number);
  return (h || 0) * 60 + (m || 0);
}
export function FlowsPage() {
  const flows = useFlows();
  const holidays = useHolidays();
  const extensions = useExtensionDirectory();
  const { canAdminister } = usePermissions();
  const { create, publish, rollback, remove } = useFlowChanges();
  const holiday = useHolidayChanges();
  const toast = useToast();
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<Flow | null>(null);
  const [versionsFor, setVersionsFor] = useState<Flow | null>(null);
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const extLabel = (id: string) => extensions.data?.find((e) => e.id === id)?.number ?? "—";
  const columns: Column<Flow>[] = [
    { key: "number", header: "Number", render: (f) => f.number, numeric: true },
    { key: "name", header: "Name", render: (f) => f.name },
    { key: "version", header: "Version", render: (f) => `v${f.activeVersion}` },
    { key: "open", header: "Open", render: (f) => extLabel(f.config.openExtensionId) },
    { key: "closed", header: "Closed", render: (f) => extLabel(f.config.closedExtensionId) },
    { key: "hours", header: "UTC hours", render: (f) => `${clock(f.config.businessStartMinute)}–${clock(f.config.businessEndMinute)}` },
  ];

  async function onCreate(input: FlowInput) {
    setBusy(true);
    setFailure(null);
    try {
      await create(input);
      setAdding(false);
      toast.show(`Call flow ${input.name} created.`);
    } catch (err) {
      setFailure(toApiError(err).message);
    } finally {
      setBusy(false);
    }
  }

  async function onPublish(flow: Flow, config: FlowConfig) {
    setBusy(true);
    setFailure(null);
    try {
      await publish({ id: flow.id, config });
      setEditing(null);
      toast.show(`Call flow ${flow.name} published as v${flow.activeVersion + 1}.`);
    } catch (err) {
      setFailure(toApiError(err).message);
    } finally {
      setBusy(false);
    }
  }

  async function onDelete(flow: Flow) {
    if (!window.confirm(`Delete call flow ${flow.name}? Its DID stops routing.`)) return;
    setBusy(true);
    try { await remove(flow.id); toast.show(`Call flow ${flow.name} deleted.`); }
    catch (err) { setFailure(toApiError(err).message); }
    finally { setBusy(false); }
  }

  return (
    <section className="page" aria-labelledby="page-title">
      <PageHeader
        title="Call flows"
        description="A DID enters a flow; open/closed destinations follow UTC business hours and holidays."
        action={canAdminister ? <button type="button" className="button button-primary" onClick={() => setAdding(true)}>Add flow</button> : undefined}
      />
      {failure ? <Alert tone="danger" title={failure} /> : null}
      {flows.isPending ? <p role="status">Loading call flows…</p> : null}
      {flows.isError ? <Alert tone="danger" title="Call flows could not be loaded"><p>{flows.error.message}</p></Alert> : null}
      {flows.data ? (
        <DataTable
          caption="Call flows of this tenant"
          captionHidden
          columns={columns}
          rows={flows.data}
          rowKey={(f) => f.id}
          rowActions={canAdminister ? (f) => (
            <div className="row-actions">
              <button type="button" className="button" onClick={() => setEditing(f)} disabled={busy}>Publish…</button>
              <button type="button" className="button" onClick={() => setVersionsFor(f)} disabled={busy}>Versions</button>
              <button type="button" className="button button-danger" onClick={() => void onDelete(f)} disabled={busy}>Delete</button>
            </div>
          ) : undefined}
          empty={<p>No call flows yet.</p>}
        />
      ) : null}

      <FlowDialog
        key={editing?.id ?? `new-${adding}`}
        open={adding || editing !== null}
        flow={editing}
        extensions={extensions.data ?? []}
        busy={busy}
        onClose={() => { setAdding(false); setEditing(null); }}
        onCreate={onCreate}
        onPublish={onPublish}
      />
      <VersionsDialog
        flow={versionsFor}
        onClose={() => setVersionsFor(null)}
        onRollback={async (flow, version) => {
          setBusy(true);
          try { await rollback({ id: flow.id, version }); toast.show(`Call flow ${flow.name} rolled back to v${version}.`); setVersionsFor(null); }
          catch (err) { setFailure(toApiError(err).message); }
          finally { setBusy(false); }
        }}
      />

      <section aria-labelledby="holidays-title" className="overview-section">
        <div className="section-header"><h2 id="holidays-title">Holidays</h2></div>
        <HolidayForm busy={busy} onAdd={async (day, name) => {
          setBusy(true);
          try { await holiday.add({ day, name }); toast.show("Holiday added."); }
          catch (err) { setFailure(toApiError(err).message); }
          finally { setBusy(false); }
        }} />
        {holidays.data?.length ? (
          <DataTable caption="Holidays" captionHidden columns={[
            { key: "day", header: "Day", render: (h) => h.day },
            { key: "name", header: "Name", render: (h) => h.name || "—" },
          ]} rows={holidays.data} rowKey={(h) => h.day} rowActions={canAdminister ? (h) => (
            <button type="button" className="button" onClick={() => void holiday.remove(h.day).catch((err: unknown) => setFailure(toApiError(err).message))} disabled={busy}>Remove</button>
          ) : undefined} empty={<p>No holidays.</p>} />
        ) : null}
      </section>
    </section>
  );
}

function FlowDialog({ open, flow, extensions, busy, onClose, onCreate, onPublish }: {
  open: boolean; flow: Flow | null; extensions: { id: string; number: string }[]; busy: boolean;
  onClose: () => void; onCreate: (i: FlowInput) => Promise<void>; onPublish: (f: Flow, c: FlowConfig) => Promise<void>;
}) {
  const titleId = useId();
  const openId = useId();
  const closedId = useId();
  const [number, setNumber] = useState(flow?.number ?? "");
  const [name, setName] = useState(flow?.name ?? "");
  const [openExt, setOpenExt] = useState(flow?.config.openExtensionId ?? "");
  const [closedExt, setClosedExt] = useState(flow?.config.closedExtensionId ?? "");
  const [start, setStart] = useState(clock(flow?.config.businessStartMinute ?? 480));
  const [end, setEnd] = useState(clock(flow?.config.businessEndMinute ?? 1080));
  const [prompt, setPrompt] = useState(flow?.config.prompt ?? "");
  const [menuText, setMenuText] = useState(
    Object.entries(flow?.config.menu ?? {}).map(([d, id]) => `${d}=${extensions.find((x) => x.id === id)?.number ?? id}`).join(", "),
  );
  const [timeoutSeconds, setTimeoutSeconds] = useState(String(flow?.config.menuTimeoutSeconds ?? 5));
  const [noInput, setNoInput] = useState(flow?.config.noInputExtensionId ?? "");
  const [invalid, setInvalid] = useState(flow?.config.invalidExtensionId ?? "");
  const menuId = useId();
  const noInputId = useId();
  const invalidId = useId();

  async function submit(ev: FormEvent) {
    ev.preventDefault();
    const menu: Record<string, string> = {};
    for (const part of menuText.split(",")) {
      const [digit, extensionNumber] = part.split("=").map((v) => v.trim());
      if (!digit || !extensionNumber) continue;
      const ext = extensions.find((x) => x.number === extensionNumber);
      if (ext) menu[digit] = ext.id;
    }
    const config = {
      openExtensionId: openExt, closedExtensionId: closedExt,
      businessStartMinute: minutes(start), businessEndMinute: minutes(end), prompt,
      menu, menuTimeoutSeconds: Number(timeoutSeconds) || 0,
      noInputExtensionId: noInput, invalidExtensionId: invalid,
    };
    if (flow) await onPublish(flow, config);
    else await onCreate({ number, name, config });
  }

  return (
    <Modal open={open} onClose={() => (busy ? undefined : onClose())} labelledBy={titleId}>
      <h2 id={titleId} className="modal-title">{flow ? `Publish ${flow.name}` : "Add a call flow"}</h2>
      <form onSubmit={(e) => void submit(e)} noValidate>
        <div className="modal-body">
          {!flow ? <TextField label="DID number" value={number} onChange={(e) => setNumber(e.target.value)} required /> : <p>DID: <strong>{flow.number}</strong></p>}
          {!flow ? <TextField label="Name" value={name} onChange={(e) => setName(e.target.value)} required /> : null}
          <div className="field">
            <label className="field-label" htmlFor={openId}>Open destination</label>
            <select id={openId} className="input" value={openExt} onChange={(e) => setOpenExt(e.target.value)}>
              <option value="">None</option>
              {extensions.map((x) => <option key={x.id} value={x.id}>{x.number}</option>)}
            </select>
          </div>
          <div className="field">
            <label className="field-label" htmlFor={closedId}>Closed destination</label>
            <select id={closedId} className="input" value={closedExt} onChange={(e) => setClosedExt(e.target.value)}>
              <option value="">None</option>
              {extensions.map((x) => <option key={x.id} value={x.id}>{x.number}</option>)}
            </select>
          </div>
          <TextField label="Open from (UTC)" type="time" value={start} onChange={(e) => setStart(e.target.value)} />
          <TextField label="Open until (UTC)" type="time" value={end} onChange={(e) => setEnd(e.target.value)} />
          <TextField label="Prompt" value={prompt} onChange={(e) => setPrompt(e.target.value)} hint="Built-in Asterisk sound reference, e.g. sound:demo-congrats. Required when a menu is set." />
          <div className="field">
            <label className="field-label" htmlFor={menuId}>Menu (digit=extension, comma-separated)</label>
            <input id={menuId} className="input" value={menuText} onChange={(e) => setMenuText(e.target.value)} placeholder="1=1001, 2=1002" />
          </div>
          <TextField label="Menu timeout (seconds)" inputMode="numeric" value={timeoutSeconds} onChange={(e) => setTimeoutSeconds(e.target.value)} />
          <div className="field">
            <label className="field-label" htmlFor={noInputId}>No input destination</label>
            <select id={noInputId} className="input" value={noInput} onChange={(e) => setNoInput(e.target.value)}>
              <option value="">Hang up</option>
              {extensions.map((x) => <option key={x.id} value={x.id}>{x.number}</option>)}
            </select>
          </div>
          <div className="field">
            <label className="field-label" htmlFor={invalidId}>Invalid input destination</label>
            <select id={invalidId} className="input" value={invalid} onChange={(e) => setInvalid(e.target.value)}>
              <option value="">Hang up</option>
              {extensions.map((x) => <option key={x.id} value={x.id}>{x.number}</option>)}
            </select>
          </div>
        </div>
        <div className="modal-actions">
          <button type="button" className="button" onClick={onClose} disabled={busy}>Cancel</button>
          <button type="submit" className="button button-primary" disabled={busy}>{flow ? "Publish version" : "Create flow"}</button>
        </div>
      </form>
    </Modal>
  );
}

function VersionsDialog({ flow, onClose, onRollback }: { flow: Flow | null; onClose: () => void; onRollback: (f: Flow, v: number) => Promise<void> }) {
  const versions = useFlowVersions(flow?.id ?? null);
  if (!flow) return null;
  return (
    <Modal open onClose={onClose} labelledBy="versions-title">
      <h2 id="versions-title" className="modal-title">Versions of {flow.name}</h2>
      <div className="modal-body">
        {versions.isPending ? <p role="status">Loading versions…</p> : null}
        <DataTable caption="Flow versions" captionHidden columns={[
          { key: "version", header: "Version", render: (v) => `v${v.version}` },
          { key: "published", header: "Published", render: (v) => v.publishedAt?.toLocaleString() ?? "—" },
        ]} rows={versions.data ?? []} rowKey={(v) => String(v.version)} rowActions={(v) => (
          v.version === flow.activeVersion ? null : (
            <button type="button" className="button" onClick={() => void onRollback(flow, v.version)}>Roll back</button>
          )
        )} empty={<p>No versions.</p>} />
      </div>
      <div className="modal-actions"><button type="button" className="button" onClick={onClose}>Close</button></div>
    </Modal>
  );
}

function HolidayForm({ busy, onAdd }: { busy: boolean; onAdd: (day: string, name: string) => Promise<void> }) {
  const dayId = useId();
  const nameId = useId();
  const [day, setDay] = useState("");
  const [name, setName] = useState("");
  return (
    <form className="filter-bar" onSubmit={(e) => { e.preventDefault(); if (day) void onAdd(day, name); setName(""); }}>
      <div className="field"><label className="field-label" htmlFor={dayId}>Day</label><input id={dayId} className="input" type="date" value={day} onChange={(e) => setDay(e.target.value)} /></div>
      <div className="field"><label className="field-label" htmlFor={nameId}>Name</label><input id={nameId} className="input" value={name} onChange={(e) => setName(e.target.value)} /></div>
      <button type="submit" className="button" disabled={busy || !day}>Add holiday</button>
    </form>
  );
}
