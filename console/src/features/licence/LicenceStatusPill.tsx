import { Link } from "react-router";
import { LicenseCondition } from "../../api/gen/atsapbx/v1/licensing_pb";
import { ToneIcon, type Tone } from "../../components/icons";

// The pill shows the server's condition exactly as reported (D-70): the
// browser maps a condition to words and a tone, and never derives one.
const presentation: Record<LicenseCondition, { label: string; tone: Tone }> = {
  [LicenseCondition.UNSPECIFIED]: { label: "Licence unknown", tone: "neutral" },
  [LicenseCondition.SETUP]: { label: "Setup", tone: "info" },
  [LicenseCondition.FREE_COMMUNITY]: { label: "Free Community", tone: "info" },
  [LicenseCondition.VALID]: { label: "Valid", tone: "success" },
  [LicenseCondition.CAPACITY_WARNING]: { label: "Capacity warning", tone: "warning" },
  [LicenseCondition.CAPACITY_REACHED]: { label: "Capacity reached", tone: "danger" },
  [LicenseCondition.ENTITLEMENT_UNVERIFIED]: { label: "Entitlement unverified", tone: "warning" },
  [LicenseCondition.DEGRADED]: { label: "Degraded", tone: "danger" },
};

export function licencePresentation(condition: LicenseCondition) {
  return presentation[condition] ?? presentation[LicenseCondition.UNSPECIFIED];
}

// LicenceStatusPill is the top bar's link to the licence page: the
// condition in words, with its icon and colour, and the licensed channels
// when there are any. Compact, not a sales banner (CONSOLE-DESIGN).
export function LicenceStatusPill({ condition, channels }: { condition: LicenseCondition; channels?: number }) {
  const { label, tone } = licencePresentation(condition);
  const detail = channels && channels > 0 ? ` · ${channels} channels` : "";
  return (
    <Link to="/licence" className={`pill pill-${tone}`} aria-label={`Licence: ${label}${detail}. Open licence details`}>
      <ToneIcon tone={tone} />
      <span>
        {label}
        {detail ? <span className="numeric">{detail}</span> : null}
      </span>
    </Link>
  );
}
