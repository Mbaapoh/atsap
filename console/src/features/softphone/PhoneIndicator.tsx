import { Link } from "react-router";
import { usePermissions } from "../auth/permissions";
import { useSoftphone } from "./SoftphoneProvider";

// PhoneIndicator is the shell's browser-phone entry and status (D-81). It
// is the way back to /phone from anywhere; an incoming call is visible
// here even when the person is on another page.
// indicatorLabel names the browser-phone state in a few words. Exported
// for a unit test of every branch.
export function indicatorLabel(phone: Pick<ReturnType<typeof useSoftphone>, "registration" | "call" | "number">): string {
  if (phone.registration === "connecting") return "Phone: connecting…";
  if (phone.registration === "error") return "Phone: unavailable";
  if (phone.registration === "registered") {
    if (phone.call === "incoming") return "Incoming call";
    if (phone.call === "in-call") return "Phone: in call";
    if (phone.call === "calling") return "Phone: calling…";
    return phone.number ? `Phone ${phone.number}` : "Browser phone";
  }
  return "Browser phone";
}

export function PhoneIndicator() {
  const { canAdminister } = usePermissions();
  const phone = useSoftphone();
  if (!canAdminister) return null;

  const label = indicatorLabel(phone);
  const tone = phone.call === "incoming" ? " phone-indicator-incoming" : phone.registration === "registered" ? " phone-indicator-on" : "";
  return (
    <Link to="/phone" className={`phone-indicator${tone}`} aria-label={label}>
      <span className="phone-dot" aria-hidden="true" />
      <span className="phone-glyph" aria-hidden="true">
        ☎
      </span>
      <span className="phone-indicator-text">{label}</span>
    </Link>
  );
}
