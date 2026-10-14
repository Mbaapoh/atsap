import type { UseQueryResult } from "@tanstack/react-query";
import { Link } from "react-router";
import type { ApiError } from "../../api/errors";
import { LicenseCondition } from "../../api/gen/atsapbx/v1/licensing_pb";
import { DataTable, type Column } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { PageHeader } from "../../components/PageHeader";
import { ToneIcon } from "../../components/icons";
import { useLicenceStatus, type LicenceSummary } from "../licence/adapter";
import { licencePresentation } from "../licence/LicenceStatusPill";
import { RECENT_CALLS, useHasCompletedCall, useHasExtension, useHasRoute, useRecentCalls, useTrunks, useUserCount, type RecentCall, type TrunkHealthRow } from "./adapter";
import { Checklist, type ChecklistItem, type ItemState } from "./Checklist";
import { directionLabel, durationLabel } from "./copy";
import { TrunkHealthCell } from "../telephony/TrunkHealthCell";

const when = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

// stateOf turns a query into a checklist state: done or to-do only when
// the fact was read; "could not check" when it was not, never a guess.
function stateOf<T>(q: UseQueryResult<T, ApiError>, isDone: (value: T) => boolean): ItemState {
  if (q.isPending) return "checking";
  if (q.isError) return "unknown";
  return isDone(q.data) ? "done" : "todo";
}

function unknownReason(...queries: UseQueryResult<unknown, ApiError>[]): string | undefined {
  const err = queries.find((q) => q.isError)?.error;
  if (!err) return undefined;
  return err.kind === "forbidden" ? "Your role cannot see this, so it was not checked." : "This could not be checked just now.";
}

function both(a: ItemState, b: ItemState): ItemState {
  if (a === "unknown" || b === "unknown") return "unknown";
  if (a === "checking" || b === "checking") return "checking";
  return a === "done" && b === "done" ? "done" : "todo";
}

// LicenceSummaryCard is the overview's compact licence summary: the
// server's condition and channels in use, linking to the licence page.
function LicenceSummaryCard({ s }: { s: LicenceSummary }) {
  const { label, tone } = licencePresentation(s.condition);
  return (
    <section className="summary-card" aria-labelledby="licence-summary-title">
      <h2 id="licence-summary-title">Licence</h2>
      <p className="summary-headline">
        <ToneIcon tone={tone} /> {label}
      </p>
      <p>{s.channels > 0 ? `${s.channelsInUse} of ${s.channels} channels in use` : "No channels licensed"}</p>
      <Link to="/licence">Licence details</Link>
    </section>
  );
}

const callColumns: Column<RecentCall>[] = [
  { key: "time", header: "Started", render: (c) => (c.startedAt ? when.format(c.startedAt) : "—") },
  { key: "direction", header: "Direction", render: (c) => directionLabel(c.direction) },
  { key: "from", header: "From", render: (c) => c.from || "—", numeric: true },
  { key: "to", header: "To", render: (c) => c.to || "—", numeric: true },
  { key: "state", header: "State", render: (c) => c.state },
  { key: "duration", header: "Duration", render: (c) => durationLabel(c.durationSeconds), numeric: true },
];

const trunkColumns: Column<TrunkHealthRow>[] = [
  { key: "name", header: "Trunk", render: (t) => t.name },
  { key: "health", header: "Observed health", render: (t) => <TrunkHealthCell health={t.health} reason={t.healthReason} observedAt={t.healthObservedAt} /> },
];

// OverviewPage is a setup and awareness surface, not a monitoring
// dashboard (CONSOLE-DESIGN "Overview"): the licence, the setup
// checklist, and the latest calls and trunk health when there are any.
// Every fact is read from a public API for the current Tenant.
export function OverviewPage() {
  const licence = useLicenceStatus();
  const users = useUserCount();
  const extension = useHasExtension();
  const trunks = useTrunks();
  const route = useHasRoute();
  const completed = useHasCompletedCall();
  const calls = useRecentCalls();

  const items: ChecklistItem[] = [
    {
      id: "licence",
      title: "Apply a licence",
      rule: "Done when a licence is in force.",
      state: stateOf(licence, (s) => s.condition !== LicenseCondition.SETUP && s.condition !== LicenseCondition.UNSPECIFIED),
      unknownReason: unknownReason(licence),
      link: { to: "/licence", label: "Licence" },
    },
    {
      id: "user",
      title: "Add a user",
      rule: "Done when this tenant has a user besides its first administrator.",
      state: stateOf(users, (n) => n > 1),
      unknownReason: unknownReason(users),
      link: { to: "/users", label: "Users" },
    },
    {
      id: "extension",
      title: "Add an extension",
      rule: "Done when this tenant has at least one extension.",
      state: stateOf(extension, Boolean),
      unknownReason: unknownReason(extension),
      link: { to: "/extensions", label: "Extensions" },
    },
    {
      id: "trunk",
      title: "Connect a trunk and a route",
      rule: "Done when this tenant has at least one trunk and one route.",
      state: both(stateOf(trunks, (t) => t.length > 0), stateOf(route, Boolean)),
      unknownReason: unknownReason(trunks, route),
      link: { to: "/trunks", label: "Trunks & routes" },
    },
    {
      id: "call",
      title: "Complete a call",
      rule: "Done when one of the latest ended calls was answered before it ended.",
      state: stateOf(completed, Boolean),
      unknownReason: unknownReason(completed),
      link: { to: "/calls", label: "Call history" },
    },
  ];

  return (
    <section className="page" aria-labelledby="page-title">
      <PageHeader title="Overview" description="Setup progress and recent activity for this tenant." />
      {licence.data ? <LicenceSummaryCard s={licence.data} /> : null}

      <section aria-labelledby="checklist-title" className="overview-section">
        <h2 id="checklist-title">Setup</h2>
        <Checklist items={items} />
      </section>

      <section aria-labelledby="calls-title" className="overview-section">
        <h2 id="calls-title">Recent calls</h2>
        {calls.isPending ? <p role="status">Loading recent calls…</p> : null}
        {calls.isError ? <p>Recent calls could not be loaded. {calls.error.kind === "forbidden" ? "Your role cannot see them." : ""}</p> : null}
        {calls.data ? (
          <DataTable
            caption={`The latest ${RECENT_CALLS} calls`}
            captionHidden
            columns={callColumns}
            rows={calls.data}
            rowKey={(c) => c.id}
            empty={<EmptyState title="No calls yet" action={<Link to="/calls">Call history</Link>}>Calls appear here once your phones start using the platform.</EmptyState>}
          />
        ) : null}
      </section>

      {trunks.data && trunks.data.length > 0 ? (
        <section aria-labelledby="trunks-title" className="overview-section">
          <h2 id="trunks-title">Trunk health</h2>
          <DataTable caption="Trunks and their observed health" captionHidden columns={trunkColumns} rows={trunks.data} rowKey={(t) => t.id} />
        </section>
      ) : null}
    </section>
  );
}
