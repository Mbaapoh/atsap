import { Link } from "react-router";
import { StatusBadge } from "../../components/StatusBadge";

// A checklist item's state, from the API read behind it.
export type ItemState = "done" | "todo" | "checking" | "unknown";

export interface ChecklistItem {
  id: string;
  title: string;
  // rule says, in words, exactly what makes the item done, so the
  // checklist never claims more than it checked.
  rule: string;
  state: ItemState;
  // unknownReason is console copy for a fact that could not be read.
  unknownReason?: string;
  link: { to: string; label: string };
}

const badge: Record<ItemState, { label: string; tone: "success" | "neutral" | "info" | "warning" }> = {
  done: { label: "Done", tone: "success" },
  todo: { label: "To do", tone: "neutral" },
  checking: { label: "Checking", tone: "info" },
  unknown: { label: "Could not check", tone: "warning" },
};

// Checklist renders the setup steps as an ordered list. Each step shows
// whether it is done, the rule that decides it, and where to do it.
export function Checklist({ items }: { items: ChecklistItem[] }) {
  return (
    <ol className="checklist" aria-label="Setup checklist">
      {items.map((item) => (
        <li key={item.id} className="checklist-item">
          <div className="checklist-text">
            <p className="checklist-title">{item.title}</p>
            <p className="checklist-rule">{item.state === "unknown" && item.unknownReason ? item.unknownReason : item.rule}</p>
          </div>
          <StatusBadge tone={badge[item.state].tone} label={badge[item.state].label} />
          <Link to={item.link.to} className="checklist-link">
            Go to {item.link.label}
          </Link>
        </li>
      ))}
    </ol>
  );
}
