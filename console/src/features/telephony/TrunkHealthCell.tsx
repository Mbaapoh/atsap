import type { TrunkHealth, TrunkHealthReason } from "../../api/gen/atsapbx/v1/pbx_pb";
import { StatusBadge } from "../../components/StatusBadge";
import { checkedAt, healthReason, trunkHealth } from "./copy";

// TrunkHealthCell shows a trunk's observed health with why and when: for
// example "Unhealthy · Host not found · checked 14:02:31". A trunk not
// checked yet has neither reason nor time, and says so.
export function TrunkHealthCell({ health, reason, observedAt }: { health: TrunkHealth; reason: TrunkHealthReason; observedAt: Date | null }) {
  const detail = [healthReason[reason], observedAt ? `checked ${checkedAt(observedAt)}` : null].filter((part) => part !== null);
  return (
    <span className="health-cell">
      <StatusBadge {...trunkHealth[health]} />
      {detail.length > 0 ? <span className="health-detail">{detail.join(" · ")}</span> : null}
    </span>
  );
}
