import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createBrowserRouter, RouterProvider } from "react-router";
import { ApiProvider } from "../api/ApiProvider";
import { ToastProvider } from "../components/Toast";
import { createApiClients, type ApiClients } from "../api/clients";
import { createApiTransport } from "../api/transport";
import { SessionController } from "../features/auth/adapter";
import { SessionProvider } from "../features/auth/SessionProvider";
import { SessionStore } from "../features/auth/session";
import { routes } from "./routes";

// Server state lives in TanStack Query and is refetched after a mutation
// rather than guessed (HLD-19 §5). Failed requests are not retried by
// default: an authorization or validation refusal does not become a
// success on the second try, and retrying a throttled sign-in would only
// extend the delay.
function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchOnWindowFocus: false },
      mutations: { retry: false },
    },
  });
}

interface Wiring {
  clients: ApiClients;
  session: SessionController;
}

// wire is the composition root: one in-memory session store, one
// transport reading its token and asking it to refresh, one set of
// clients, and the session controller over the auth client. A change of
// person or Tenant empties the query cache, so nothing fetched in one
// scope is shown in another.
function wire(queryClient: QueryClient): Wiring {
  const store = new SessionStore();
  let session: SessionController | undefined;
  const clients = createApiClients(
    createApiTransport({
      accessToken: store.accessToken,
      refresh: () => session?.refresh() ?? Promise.resolve(false),
    }),
  );
  session = new SessionController(clients.auth, store, { onScopeChange: () => queryClient.clear() });
  return { clients, session };
}

export function App() {
  const [queryClient] = useState(createQueryClient);
  const [{ clients, session }] = useState(() => wire(queryClient));
  const [router] = useState(() => createBrowserRouter(routes));

  return (
    <QueryClientProvider client={queryClient}>
      <ApiProvider clients={clients}>
        <SessionProvider controller={session}>
          <ToastProvider>
            <RouterProvider router={router} />
          </ToastProvider>
        </SessionProvider>
      </ApiProvider>
    </QueryClientProvider>
  );
}
