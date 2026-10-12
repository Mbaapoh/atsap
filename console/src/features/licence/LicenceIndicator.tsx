import { LicenseCondition } from "../../api/gen/atsapbx/v1/licensing_pb";
import { useLicenceStatus } from "./adapter";
import { LicenceStatusPill } from "./LicenceStatusPill";

// LicenceIndicator is the top bar's licence pill, fed by the server. While
// the status is loading nothing is shown; if it cannot be read the pill
// says "Licence unknown" rather than guessing.
export function LicenceIndicator() {
  const status = useLicenceStatus();
  if (status.isPending) return null;
  if (status.isError) return <LicenceStatusPill condition={LicenseCondition.UNSPECIFIED} />;
  return <LicenceStatusPill condition={status.data.condition} channels={status.data.channels} />;
}
