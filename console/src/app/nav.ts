// Primary navigation for the Phase A console (CONSOLE-DESIGN "Navigation
// rules", HLD-19 §5): the five workflow screens. Licence is reached from
// the top bar's status pill, not from here (owner decision, 2026-10-12);
// Account is a utility link. No dormant entries for Phase B work.
export interface NavItem {
  path: string;
  label: string;
}

export const primaryNav: NavItem[] = [
  { path: "/", label: "Overview" },
  { path: "/users", label: "Users" },
  { path: "/extensions", label: "Extensions" },
  { path: "/trunks", label: "Trunks & routes" },
  { path: "/calls", label: "Call history" },
];

