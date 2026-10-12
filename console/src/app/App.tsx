import { useState } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createBrowserRouter, RouterProvider } from "react-router";
import { ApiProvider } from "../api/ApiProvider";
import { ToastProvider } from "../components/Toast";
import { createApiClients } from "../api/clients";
import { createApiTransport } from "../api/transport";
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

export function App() {
  const [queryClient] = useState(createQueryClient);
  // The access token source is wired by the session feature (task 2.1).
  const [clients] = useState(() => createApiClients(createApiTransport()));
  const [router] = useState(() => createBrowserRouter(routes));

  return (
    <QueryClientProvider client={queryClient}>
      <ApiProvider clients={clients}>
        <ToastProvider>
          <RouterProvider router={router} />
        </ToastProvider>
      </ApiProvider>
    </QueryClientProvider>
  );
}
