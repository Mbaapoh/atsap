import { Code, ConnectError, type Interceptor, type Transport } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { isPublicProcedure } from "./publicApi";

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

// publicOnly refuses, before anything is sent, a request that is not a
// public procedure on the console's own origin (BRD §16: the console uses
// only the public API). Generated clients cannot produce one; this keeps
// it that way if a hand-built call ever slips in.
function publicOnly(origin: string): Interceptor {
  return (next) => async (req) => {
    if (!isPublicProcedure(req.url, origin)) {
      throw new ConnectError("request outside the public API refused", Code.PermissionDenied);
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
    interceptors: [publicOnly(baseUrl), ...(options.accessToken ? [bearer(options.accessToken)] : [])],
  });
}
