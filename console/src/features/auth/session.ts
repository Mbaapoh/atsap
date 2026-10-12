// The console's session, held in memory and nowhere else (HLD-19 §4,
// console brief §6 rules 1–3). A reload loses it on purpose: the HttpOnly
// refresh cookie, which the page cannot read, restores it through
// RefreshSession. Nothing here is written to local or session storage, a
// URL, history state, a log or telemetry.

export interface Membership {
  tenantId: string;
  tenantName: string;
  principalId: string;
  roles: string[];
}

export interface AccessGrant {
  // token is credential material: it leaves this module only as the
  // Authorization header the transport adds.
  token: string;
  expiresAt: Date | null;
  tenantId: string;
  principalId: string;
}

// SessionStatus is where the person stands:
//  - restoring: on load, before the refresh cookie has been tried;
//  - signed-out: no session (endedReason says why, when one just ended);
//  - choosing-tenant: signed in, several memberships, none chosen yet;
//  - no-access: signed in but with no active membership;
//  - signed-in: holding an access token for one Tenant.
export type SessionStatus = "restoring" | "signed-out" | "choosing-tenant" | "no-access" | "signed-in";

// EndedReason says why the person is signed out, for the sign-in page.
//  - expired: the server would no longer renew it (idle or absolute
//    limit reached, signed out elsewhere, or a password change);
//  - signed-out: the person signed out;
//  - unavailable: the server could not be reached to restore it.
export type EndedReason = "expired" | "signed-out" | "unavailable";

export interface SessionState {
  status: SessionStatus;
  access: AccessGrant | null;
  memberships: Membership[];
  passwordChangeRequired: boolean;
  sessionExpiresAt: Date | null;
  endedReason: EndedReason | null;
}

export const initialSession: SessionState = {
  status: "restoring",
  access: null,
  memberships: [],
  passwordChangeRequired: false,
  sessionExpiresAt: null,
  endedReason: null,
};

// SessionStore is a minimal external store for useSyncExternalStore.
export class SessionStore {
  private state: SessionState;
  private readonly listeners = new Set<() => void>();

  constructor(state: SessionState = initialSession) {
    this.state = state;
  }

  get = (): SessionState => this.state;

  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  };

  // accessToken is the transport's token source.
  accessToken = (): string | undefined => this.state.access?.token;

  set(next: SessionState): void {
    this.state = next;
    for (const l of this.listeners) l();
  }

  // clear drops every credential held in memory.
  clear(endedReason: EndedReason | null = null): void {
    this.set({ ...initialSession, status: "signed-out", endedReason });
  }

  currentMembership(): Membership | undefined {
    const tenantId = this.state.access?.tenantId;
    return this.state.memberships.find((m) => m.tenantId === tenantId);
  }
}

// statusFor decides the status a fresh grant puts the person in.
export function statusFor(access: AccessGrant | null, memberships: Membership[]): SessionStatus {
  if (access) return "signed-in";
  if (memberships.length === 0) return "no-access";
  return "choosing-tenant";
}
