import type { RouteObject } from "react-router";
import { AppShell } from "./AppShell";
import { NotFound, RoutePlaceholder } from "./RoutePlaceholder";

// The Phase A route tree (console brief §2, CONSOLE-DESIGN "Routes").
// Screens are placeholders until their tasks land; the Setup gate, session
// guard and permission checks arrive with console-foundation tasks 2.x.
export const routes: RouteObject[] = [
  { path: "/sign-in", element: <RoutePlaceholder title="Sign in" /> },
  {
    element: <AppShell />,
    children: [
      { index: true, element: <RoutePlaceholder title="Overview" /> },
      { path: "licence", element: <RoutePlaceholder title="Licence" /> },
      { path: "users", element: <RoutePlaceholder title="Users" /> },
      { path: "extensions", element: <RoutePlaceholder title="Extensions" /> },
      { path: "trunks", element: <RoutePlaceholder title="Trunks & routes" /> },
      { path: "calls", element: <RoutePlaceholder title="Call history" /> },
      { path: "account", element: <RoutePlaceholder title="Account" /> },
      { path: "*", element: <NotFound /> },
    ],
  },
];
