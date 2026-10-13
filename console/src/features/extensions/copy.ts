import { DeviceType } from "../../api/gen/atsapbx/v1/pbx_pb";
import type { Tone } from "../../components/icons";
import type { Registration } from "./adapter";

export const deviceLabel: Record<DeviceType, string> = {
  [DeviceType.UNSPECIFIED]: "Unknown",
  [DeviceType.SIP]: "Desk or soft phone (SIP)",
  [DeviceType.WEBRTC]: "Browser phone (WebRTC)",
};

// Registration is observed device state, never a setting (CONSOLE-DESIGN
// "Extensions"). Unknown is not "Not registered": the server did not say.
export const registrationBadge: Record<Registration, { label: string; tone: Tone }> = {
  registered: { label: "Registered", tone: "success" },
  "not-registered": { label: "Not registered", tone: "neutral" },
  unknown: { label: "Unknown", tone: "warning" },
};
