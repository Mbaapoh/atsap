import { TrunkHealth } from "../../api/gen/atsapbx/v1/pbx_pb";
import type { Tone } from "../../components/icons";

// Observed trunk health, in words. Health is what the platform has
// observed from real calls, never a setting (CONSOLE-DESIGN "Trunks").
export const trunkHealth: Record<TrunkHealth, { label: string; tone: Tone }> = {
  [TrunkHealth.UNSPECIFIED]: { label: "Not yet observed", tone: "neutral" },
  [TrunkHealth.HEALTHY]: { label: "Healthy", tone: "success" },
  [TrunkHealth.DEGRADED]: { label: "Degraded", tone: "warning" },
  [TrunkHealth.UNHEALTHY]: { label: "Unhealthy", tone: "danger" },
};
