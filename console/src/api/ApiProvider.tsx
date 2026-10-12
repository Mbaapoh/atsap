import { createContext, useContext, type ReactNode } from "react";
import type { ApiClients } from "./clients";

const ApiContext = createContext<ApiClients | null>(null);

export function ApiProvider({ clients, children }: { clients: ApiClients; children: ReactNode }) {
  return <ApiContext.Provider value={clients}>{children}</ApiContext.Provider>;
}

// useApiClients is for feature adapters only; visual components receive
// data and callbacks, never clients (console brief §7).
export function useApiClients(): ApiClients {
  const clients = useContext(ApiContext);
  if (!clients) {
    throw new Error("useApiClients must be used inside ApiProvider");
  }
  return clients;
}
