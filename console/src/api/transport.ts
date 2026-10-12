import type { Interceptor, Transport } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

export type AccessTokenSource = () => string | undefined;

export interface ApiTransportOptions {
  // The API's origin. Production and development both serve the console and
  // the API from one origin (D-69), so this defaults to the page's own.
  baseUrl?: string;
  // Reads the in-memory access token (HLD-19 §4). It is never stored in
  // browser persistence or a URL; the session feature owns it.
  accessToken?: AccessTokenSource;
}

// bearer adds the access token to every call when one is held. Calls that
// need none (sign-in, refresh, recovery) simply go without it; the server
// decides what each procedure requires.
function bearer(source: AccessTokenSource): Interceptor {
  return (next) => async (req) => {
    const token = source();
    if (token) {
      req.header.set("Authorization", `Bearer ${token}`);
    }
    return next(req);
  };
}

export function createApiTransport(options: ApiTransportOptions = {}): Transport {
  const baseUrl = options.baseUrl ?? window.location.origin;
  return createConnectTransport({
    baseUrl,
    // Same-origin requests send the HttpOnly refresh cookie that the server
    // scopes to AuthService. Nothing in the page can read it.
    fetch: (input, init) => globalThis.fetch(input, { ...init, credentials: "same-origin" }),
    interceptors: options.accessToken ? [bearer(options.accessToken)] : [],
  });
}
