import { ToneIcon, type Tone } from "./icons";

// StatusBadge shows an observed domain state: text, icon and colour
// together. The label is the real domain word — Registered, Healthy,
// Degraded — never a generic "OK"/"Error" (CONSOLE-DESIGN "Design-system
// minimum").
export function StatusBadge({ tone, label }: { tone: Tone; label: string }) {
  return (
    <span className={`badge badge-${tone}`}>
      <ToneIcon tone={tone} />
      <span>{label}</span>
    </span>
  );
}
