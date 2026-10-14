import { useId, useState, type FormEvent } from "react";
import { useSearchParams } from "react-router";
import { Alert } from "../../components/Alert";
import { DataTable, type Column } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { PageHeader } from "../../components/PageHeader";
import { Pager } from "../../components/Pager";
import { TextField } from "../../components/TextField";
import { useCallHistory, type CallRow } from "./adapter";
import { directionLabel, durationLabel, serverText } from "./copy";
import { CALL_STATES, DIRECTIONS, readFilters, writeFilters, type CallFilters } from "./filters";

const when = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "medium" });

const columns: Column<CallRow>[] = [
  { key: "time", header: "Started", render: (c) => (c.startedAt ? when.format(c.startedAt) : "—") },
  { key: "direction", header: "Direction", render: (c) => directionLabel(c.direction) },
  { key: "from", header: "From", render: (c) => (c.from ? serverText(c.from) : "—"), numeric: true },
  { key: "to", header: "To", render: (c) => (c.to ? serverText(c.to) : "—"), numeric: true },
  // The state exactly as the server names it (PRD §11.1); never a
  // console-made Answered, Missed or Failed.
  { key: "state", header: "State", render: (c) => c.state },
  { key: "duration", header: "Duration", render: (c) => durationLabel(c.durationSeconds), numeric: true },
  { key: "reason", header: "Termination reason", render: (c) => (c.terminationReason ? serverText(c.terminationReason) : "—") },
];

// CallsPage is HLD-18 flow A5: the tenant's call history, read-only, with
// filters held in the address and sent to ListCalls together.
export function CallsPage() {
  const [params, setParams] = useSearchParams();
  const applied = readFilters(params);
  const calls = useCallHistory(applied);
  const [draft, setDraft] = useState<CallFilters>(applied);
  const [rangeError, setRangeError] = useState<string | undefined>();
  const directionId = useId();
  const stateId = useId();
  const key = params.toString();

  // Back, Forward and reload change the address: the form follows it,
  // adjusted while rendering (React's pattern for state derived from a
  // changed input) rather than in an effect or by remounting the form,
  // which would take focus from the Apply button.
  const [draftFor, setDraftFor] = useState(key);
  if (draftFor !== key) {
    setDraftFor(key);
    setDraft(applied);
    setRangeError(undefined);
  }

  function onApply(ev: FormEvent) {
    ev.preventDefault();
    if (draft.from && draft.to && draft.to < draft.from) {
      setRangeError("The end date is before the start date.");
      return;
    }
    setRangeError(undefined);
    setParams(writeFilters({ ...draft, page: 0 }));
  }

  const filtered = applied.from || applied.to || applied.direction || applied.state;

  return (
    <section className="page" aria-labelledby="page-title">
      <PageHeader title="Call history" description="Calls of this tenant, newest first, as the platform recorded them." />
      <form className="filters" onSubmit={onApply} noValidate aria-label="Call filters">
        <TextField label="From" type="date" value={draft.from} onChange={(e) => setDraft({ ...draft, from: e.target.value })} />
        <TextField label="To" type="date" value={draft.to} onChange={(e) => setDraft({ ...draft, to: e.target.value })} error={rangeError} />
        <div className="field">
          <label className="field-label" htmlFor={directionId}>
            Direction
          </label>
          <select id={directionId} className="input" value={draft.direction} onChange={(e) => setDraft({ ...draft, direction: e.target.value })}>
            <option value="">Any</option>
            {DIRECTIONS.map((d) => (
              <option key={d} value={d}>
                {directionLabel(d)}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label className="field-label" htmlFor={stateId}>
            State
          </label>
          <select id={stateId} className="input" value={draft.state} onChange={(e) => setDraft({ ...draft, state: e.target.value })}>
            <option value="">Any</option>
            {CALL_STATES.map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
        </div>
        <div className="filters-actions">
          <button type="submit" className="button button-primary">
            Apply filters
          </button>
          {filtered ? (
            <button type="button" className="button" onClick={() => setParams(new URLSearchParams())}>
              Clear filters
            </button>
          ) : null}
        </div>
      </form>
      {calls.isPending ? <p role="status">Loading calls…</p> : null}
      {calls.isError ? (
        <Alert tone="danger" title="Calls could not be loaded">
          <p>{calls.error.kind === "forbidden" ? "Your role cannot see this tenant's calls." : calls.error.message}</p>
        </Alert>
      ) : null}
      {calls.data ? (
        <>
          <DataTable
            caption="Calls of this tenant"
            captionHidden
            columns={columns}
            rows={calls.data}
            rowKey={(c) => c.id}
            empty={
              filtered ? (
                <EmptyState title="No calls match these filters">Widen the dates or clear a filter.</EmptyState>
              ) : (
                <EmptyState title="No calls yet">Calls appear here once your phones start using the platform.</EmptyState>
              )
            }
          />
          <Pager page={applied.page} rowCount={calls.data.length} onPage={(p) => setParams(writeFilters({ ...applied, page: p }))} label="Call history pages" />
        </>
      ) : null}
    </section>
  );
}
