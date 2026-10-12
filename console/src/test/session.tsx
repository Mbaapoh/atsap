import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render } from "@testing-library/react";
import { createMemoryRouter, RouterProvider, type RouteObject } from "react-router";
import { ApiProvider } from "../api/ApiProvider";
import { createApiClients } from "../api/clients";
import { createApiTransport } from "../api/transport";
import { SessionContext, SessionController } from "../features/auth/adapter";
import { initialSession, SessionStore, type SessionState } from "../features/auth/session";

export const TEST_BASE = "http://localhost:3000";

export const tenantA = { tenantId: "11111111-1111-4111-8111-111111111111", tenantName: "Acme", principalId: "p-a", roles: ["TENANT_ADMIN"] };
export const tenantB = { tenantId: "22222222-2222-4222-8222-222222222222", tenantName: "Globex", principalId: "p-b", roles: ["TENANT_ADMIN"] };

// signedIn is a session holding a token for tenantA.
export function signedIn(overrides: Partial<SessionState> = {}): SessionState {
  return {
    ...initialSession,
    status: "signed-in",
    access: { token: "access-in-memory", expiresAt: null, tenantId: tenantA.tenantId, principalId: tenantA.principalId },
    memberships: [tenantA],
    ...overrides,
  };
}

// renderApp renders routes at path with the real session controller and
// transport (requests go to MSW at TEST_BASE), starting from a given
// in-memory session instead of restoring one.
export function renderApp(routes: RouteObject[], path: string, state: SessionState = signedIn()) {
  const store = new SessionStore(state);
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  let controller: SessionController | undefined;
  const clients = createApiClients(
    createApiTransport({ baseUrl: TEST_BASE, accessToken: store.accessToken, refresh: () => controller?.refresh() ?? Promise.resolve(false) }),
  );
  controller = new SessionController(clients.auth, store, { onScopeChange: () => queryClient.clear() });
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  const view = render(
    <QueryClientProvider client={queryClient}>
      <ApiProvider clients={clients}>
        <SessionContext.Provider value={controller}>
          <RouterProvider router={router} />
        </SessionContext.Provider>
      </ApiProvider>
    </QueryClientProvider>,
  );
  return { ...view, router, store, controller, clients, queryClient };
}
