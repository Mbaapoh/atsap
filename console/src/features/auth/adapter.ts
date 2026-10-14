import { timestampDate, type Timestamp } from "@bufbuild/protobuf/wkt";
import { createContext, useContext, useSyncExternalStore } from "react";
import type { ApiClients } from "../../api/ApiProvider";
import { ApiError, callApi } from "../../api/errors";
import { codePoints } from "../../components/text";
import {
  SignInMethod,
  type AccessToken,
  type Membership as WireMembership,
  type RefreshSessionResponse,
  type SignInResponse,
} from "../../api/gen/atsapbx/v1/auth_pb";
import { statusFor, type AccessGrant, type Membership, type SessionState, type SessionStore } from "./session";

// The auth feature adapter: the only code that calls AuthService. It keeps
// the in-memory session current and is the one place a refresh happens
// (HLD-19 §4).

// SignInMethodView is how an email signs in, as the console shows it.
export type SignInMethodView = { kind: "password" } | { kind: "sso"; providerName: string };

// PASSWORD_MIN and PASSWORD_MAX mirror the server's bounds so the form can
// say what is wrong before sending; the server still decides.
export const PASSWORD_MIN = 8;
export const PASSWORD_MAX = 64;

// passwordProblem returns why a new password would be refused, or null.
export function passwordProblem(password: string): string | null {
  const n = codePoints(password);
  if (n < PASSWORD_MIN) return `Use at least ${PASSWORD_MIN} characters.`;
  if (n > PASSWORD_MAX) return `Use no more than ${PASSWORD_MAX} characters.`;
  return null;
}

function dateOrNull(ts: Timestamp | undefined): Date | null {
  return ts ? timestampDate(ts) : null;
}

function toMembership(m: WireMembership): Membership {
  return { tenantId: m.tenantId, tenantName: m.tenantName, principalId: m.principalId, roles: [...m.roles] };
}

function toGrant(a: AccessToken | undefined): AccessGrant | null {
  if (!a || !a.token) return null;
  return { token: a.token, expiresAt: dateOrNull(a.expiresAt), tenantId: a.tenantId, principalId: a.principalId };
}

function toState(res: SignInResponse | RefreshSessionResponse): SessionState {
  const access = toGrant(res.access);
  const memberships = res.memberships.map(toMembership);
  return {
    status: statusFor(access, memberships),
    access,
    memberships,
    passwordChangeRequired: res.passwordChangeRequired,
    sessionExpiresAt: dateOrNull(res.sessionExpiresAt),
    endedReason: null,
  };
}

// withRefreshLock serializes refreshes across this browser's tabs. They
// share one cookie: two tabs refreshing with the same secret at once would
// look like a replay and end the session (D-62). Where the Web Locks API
// is missing, the call runs unguarded.
async function withRefreshLock<T>(run: () => Promise<T>): Promise<T> {
  const locks = (globalThis.navigator as Navigator | undefined)?.locks;
  if (!locks) return run();
  return locks.request("atsapbx-session-refresh", run);
}

// FRESH_MARGIN_MS is how close to expiry a token is renewed before a call
// that must not be retried.
const FRESH_MARGIN_MS = 60_000;

export interface SessionControllerOptions {
  // onScopeChange runs whenever the person or Tenant behind the token
  // changes (sign-in, Tenant switch, sign-out, session end), so no
  // server state from one scope is shown in another.
  onScopeChange?: () => void;
}

export class SessionController {
  private inflight: Promise<boolean> | null = null;
  private restoring: Promise<void> | null = null;

  constructor(
    private readonly auth: ApiClients["auth"],
    readonly store: SessionStore,
    private readonly options: SessionControllerOptions = {},
  ) {}

  private scopeChanged(): void {
    this.options.onScopeChange?.();
  }

  // restore tries the refresh cookie once on load.
  restore(): Promise<void> {
    this.restoring ??= (async () => {
      try {
        const res = await callApi(() => withRefreshLock(() => this.auth.refreshSession({})));
        this.store.set(toState(res));
      } catch (err) {
        this.store.clear(err instanceof ApiError && err.kind === "unavailable" ? "unavailable" : null);
      }
    })();
    return this.restoring;
  }

  async discover(email: string): Promise<SignInMethodView> {
    const res = await callApi(() => this.auth.discoverSignIn({ email }));
    return res.method === SignInMethod.SSO
      ? { kind: "sso", providerName: res.providerDisplayName }
      : { kind: "password" };
  }

  async signIn(email: string, password: string): Promise<void> {
    const res = await callApi(() => this.auth.signIn({ email, password }));
    this.scopeChanged();
    this.store.set(toState(res));
  }

  // chooseTenant issues an access token for one of the person's
  // memberships. Switching Tenant is the same call.
  async chooseTenant(tenantId: string): Promise<void> {
    const res = await callApi(() => withRefreshLock(() => this.auth.refreshSession({ tenantId })));
    this.scopeChanged();
    this.store.set(toState(res));
  }

  // refresh renews the access token for the current Tenant after the
  // server refused it. Concurrent callers share one attempt; a failure
  // ends the session in memory and the route guard returns to sign-in.
  refresh = (): Promise<boolean> => {
    this.inflight ??= (async () => {
      try {
        const res = await callApi(() => withRefreshLock(() => this.auth.refreshSession({})));
        this.store.set(toState(res));
        return this.store.get().status === "signed-in";
      } catch {
        this.scopeChanged();
        this.store.clear("expired");
        return false;
      } finally {
        this.inflight = null;
      }
    })();
    return this.inflight;
  };

  // signOut ends the session on the server, then forgets it here whether
  // or not the server could be reached: the person asked to leave.
  async signOut(): Promise<void> {
    try {
      await callApi(() => this.auth.signOut({}));
    } finally {
      this.scopeChanged();
      this.store.clear("signed-out");
    }
  }

  // ensureFresh renews a token that expires within FRESH_MARGIN_MS, for
  // calls whose refusal cannot be told apart from an expired token.
  private async ensureFresh(): Promise<void> {
    const expiresAt = this.store.get().access?.expiresAt;
    if (expiresAt && expiresAt.getTime() - Date.now() < FRESH_MARGIN_MS) {
      await this.refresh();
    }
  }

  // changePassword checks the current password and sets the new one. The
  // server ends every other session of the person; this one continues,
  // and is renewed so a must-change token becomes a normal one.
  async changePassword(currentPassword: string, newPassword: string): Promise<void> {
    await this.ensureFresh();
    await callApi(() => this.auth.changePassword({ currentPassword, newPassword }));
    const res = await callApi(() => withRefreshLock(() => this.auth.refreshSession({})));
    this.store.set(toState(res));
  }

  async requestPasswordReset(email: string): Promise<void> {
    await callApi(() => this.auth.requestPasswordReset({ email }));
  }

  async completePasswordReset(secret: string, newPassword: string): Promise<void> {
    await callApi(() => this.auth.completePasswordReset({ secret, newPassword }));
  }
}

export const SessionContext = createContext<SessionController | null>(null);

export function useSessionController(): SessionController {
  const controller = useContext(SessionContext);
  if (!controller) throw new Error("useSessionController must be used inside SessionContext");
  return controller;
}

export function useSession(): SessionState {
  const { store } = useSessionController();
  return useSyncExternalStore(store.subscribe, store.get, store.get);
}
