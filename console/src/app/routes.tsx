import type { RouteObject } from "react-router";
import { AccountPage } from "../features/auth/AccountPage";
import { ForgotPasswordPage } from "../features/auth/ForgotPasswordPage";
import { RequireSession } from "../features/auth/RequireSession";
import { ResetPasswordPage } from "../features/auth/ResetPasswordPage";
import { SelectTenantPage } from "../features/auth/SelectTenantPage";
import { SessionExpiryNotice } from "../features/auth/SessionExpiryNotice";
import { SignInPage } from "../features/auth/SignInPage";
import { TenantIndicator } from "../features/auth/TenantIndicator";
import { LicenceIndicator } from "../features/licence/LicenceIndicator";
import { LicencePage } from "../features/licence/LicencePage";
import { RequireLicence } from "../features/licence/RequireLicence";
import { OverviewPage } from "../features/overview/OverviewPage";
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
        <AppShell tenant={<TenantIndicator />} licence={<LicenceIndicator />} notice={<SessionExpiryNotice />} />
      </RequireSession>
    ),
    children: [
      { index: true, element: <OverviewPage /> },
      { path: "licence", element: <LicencePage /> },
      { path: "users", element: <RoutePlaceholder title="Users" /> },
      {
        path: "extensions",
        element: (
          <RequireLicence area="Extensions">
            <RoutePlaceholder title="Extensions" />
          </RequireLicence>
        ),
      },
      {
        path: "trunks",
        element: (
          <RequireLicence area="Trunks and routes">
            <RoutePlaceholder title="Trunks & routes" />
          </RequireLicence>
        ),
      },
      { path: "calls", element: <RoutePlaceholder title="Call history" /> },
      { path: "account", element: <AccountPage /> },
      { path: "*", element: <NotFound /> },
    ],
  },
];
