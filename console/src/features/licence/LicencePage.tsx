import { useLocation } from "react-router";
import { Alert } from "../../components/Alert";
import { PageHeader } from "../../components/PageHeader";
import { ToneIcon } from "../../components/icons";
import { useLicenceStatus, type LicenceSummary } from "./adapter";
import { ApplyLicenceForm } from "./ApplyLicenceForm";
import { capacityLabel, conditionExplanation, reasonText, stateLabel, tierLabel } from "./copy";
import { licencePresentation } from "./LicenceStatusPill";
import { stateString } from "../auth/routerState";
import { useCanApplyLicence } from "./permissions";

const when = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" });

function Facts({ s }: { s: LicenceSummary }) {
  const rows: [string, string][] = [
    ["Tier", tierLabel[s.tier]],
    ["Entitlement state", stateLabel[s.state]],
    ["Capacity", capacityLabel[s.capacity]],
    ["Edition", s.edition || "None"],
    ["Channels", s.channels > 0 ? `${s.channelsInUse} in use of ${s.channels}` : "None"],
    ["Tenants allowed", s.maxTenants === null ? "Unlimited" : String(s.maxTenants)],
    ["Extensions allowed", s.maxExtensions === null ? "Unlimited" : String(s.maxExtensions)],
    ["Expires", s.expiresAt ? when.format(s.expiresAt) : "Does not expire"],
    ["Last confirmed", s.lastConfirmedAt ? when.format(s.lastConfirmedAt) : "Not yet confirmed"],
  ];
  const reason = s.reason ? reasonText(s.reason) : null;
  if (reason) rows.push(["Reason", reason]);
  return (
    <dl className="facts">
      {rows.map(([term, value]) => (
        <div key={term} className="facts-row">
          <dt>{term}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  );
}

// LicencePage shows every authenticated person the licence and capacity
// state exactly as the server reports it (CONSOLE-DESIGN "Licence status
// and activation"), and a platform administrator the way to apply a key.
export function LicencePage() {
  const status = useLicenceStatus();
  const canApply = useCanApplyLicence();
  const blocked = stateString(useLocation().state, "blocked");

  return (
    <section className="page" aria-labelledby="page-title">
      <PageHeader title="Licence" />
      {blocked ? (
        <Alert tone="info" title={`${blocked} open once a licence is in force`}>
          <p>{canApply ? "Apply a licence key below." : "Ask your platform administrator to apply a licence."}</p>
        </Alert>
      ) : null}
      {status.isPending ? <p role="status">Loading the licence status…</p> : null}
      {status.isError ? (
        <Alert tone="danger" title="The licence status could not be loaded">
          <p>{status.error.message}</p>
        </Alert>
      ) : null}
      {status.data ? <LicenceStatus s={status.data} /> : null}
      {canApply ? <ApplyLicenceForm /> : null}
    </section>
  );
}

function LicenceStatus({ s }: { s: LicenceSummary }) {
  const { label, tone } = licencePresentation(s.condition);
  return (
    <>
      <div className={`licence-headline licence-${tone}`} role="status">
        <ToneIcon tone={tone} />
        <div>
          <p className="licence-condition">{label}</p>
          <p>{conditionExplanation(s)}</p>
        </div>
      </div>
      <Facts s={s} />
    </>
  );
}
