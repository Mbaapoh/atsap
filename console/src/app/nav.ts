// Primary navigation for the Phase A console (CONSOLE-DESIGN "Navigation
// rules", HLD-19 §5). No dormant entries for Phase B work.
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
  { path: "/licence", label: "Licence" },
];

