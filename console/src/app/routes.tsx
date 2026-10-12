import type { RouteObject } from "react-router";
import { AccountPage } from "../features/auth/AccountPage";
import { ForgotPasswordPage } from "../features/auth/ForgotPasswordPage";
import { RequireSession } from "../features/auth/RequireSession";
import { ResetPasswordPage } from "../features/auth/ResetPasswordPage";
import { SelectTenantPage } from "../features/auth/SelectTenantPage";
import { SessionExpiryNotice } from "../features/auth/SessionExpiryNotice";
import { SignInPage } from "../features/auth/SignInPage";
import { TenantIndicator } from "../features/auth/TenantIndicator";
import { AppShell } from "./AppShell";
import { NotFound, RoutePlaceholder } from "./RoutePlaceholder";

// The Phase A route tree (console brief §2, CONSOLE-DESIGN "Routes").
// Sign-in, recovery and Tenant choice sit outside the shell; everything
// else needs a session with a chosen Tenant. /reset and /invite are the
// paths of the server's emailed links (LLD-02 §6).
export const routes: RouteObject[] = [
  { path: "/sign-in", element: <SignInPage /> },
  { path: "/select-tenant", element: <SelectTenantPage /> },
  { path: "/forgot-password", element: <ForgotPasswordPage /> },
  { path: "/reset", element: <ResetPasswordPage purpose="reset" /> },
  { path: "/invite", element: <ResetPasswordPage purpose="invite" /> },
  {
    element: (
      <RequireSession>
        <AppShell tenant={<TenantIndicator />} notice={<SessionExpiryNotice />} />
      </RequireSession>
    ),
    children: [
      { index: true, element: <RoutePlaceholder title="Overview" /> },
      { path: "licence", element: <RoutePlaceholder title="Licence" /> },
      { path: "users", element: <RoutePlaceholder title="Users" /> },
      { path: "extensions", element: <RoutePlaceholder title="Extensions" /> },
      { path: "trunks", element: <RoutePlaceholder title="Trunks & routes" /> },
      { path: "calls", element: <RoutePlaceholder title="Call history" /> },
      { path: "account", element: <AccountPage /> },
      { path: "*", element: <NotFound /> },
    ],
  },
];
