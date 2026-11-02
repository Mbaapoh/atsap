import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useSession } from "../auth/adapter";
import { callKeys, useCallSnapshot, useHangupCall, type CallSnapshot } from "./adapter";
import { serverText } from "./copy";

// A placed call is a session, not a dialog and a toast (console-call-session,
// D-79). The provider is mounted inside the authenticated shell, so it
// outlives every page and keeps the call and its hang-up control visible
// while the administrator navigates.

export type SessionPhase = "starting" | "ringing-caller" | "calling-destination" | "connected" | "ending" | "ended";

export interface SessionOrigin {
  id: string;
  number: string;
  name: string;
}

export interface CallSession {
  callId: string;
  from: SessionOrigin;
  destination: string;
  startedAt: Date;
}

interface SessionState {
  session: CallSession | null;
  phase: SessionPhase;
  snapshot: CallSnapshot | null;
  // reconnecting is true after the poll failed while the call is live; it
  // never turns a poll failure into a call failure.
  reconnecting: boolean;
  actionError: string | null;
  busy: boolean;
}

export interface CallSessionApi extends SessionState {
  start: (session: CallSession) => void;
  end: () => Promise<void>;
  dismiss: () => void;
}

const SessionContext = createContext<CallSessionApi | null>(null);

function storageKey(tenantId: string): string {
  return `atsapbx.call-session.${tenantId}`;
}

// Only the call id and the two display names are persisted: none is a
// credential and none is an engine identifier. The access token stays in
// memory (HLD-19 §4).
function readStored(tenantId: string): CallSession | null {
  if (!tenantId || typeof sessionStorage === "undefined") return null;
  try {
    const raw = sessionStorage.getItem(storageKey(tenantId));
    if (!raw) return null;
    const v: unknown = JSON.parse(raw);
    if (typeof v !== "object" || v === null) return null;
    if (!("callId" in v) || typeof v.callId !== "string") return null;
    if (!("destination" in v) || typeof v.destination !== "string") return null;
    if (!("from" in v) || typeof v.from !== "object" || v.from === null) return null;
    const from = v.from;
    if (!("id" in from) || typeof from.id !== "string") return null;
    if (!("number" in from) || typeof from.number !== "string") return null;
    if (!("name" in from) || typeof from.name !== "string") return null;
    return {
      callId: v.callId,
      from: { id: from.id, number: from.number, name: from.name },
      destination: v.destination,
      startedAt: "startedAt" in v && typeof v.startedAt === "string" ? new Date(v.startedAt) : new Date(),
    };
  } catch {
    return null;
  }
}

function writeStored(tenantId: string, session: CallSession): void {
  if (!tenantId || typeof sessionStorage === "undefined") return;
  try {
    sessionStorage.setItem(storageKey(tenantId), JSON.stringify({ ...session, startedAt: session.startedAt.toISOString() }));
  } catch {
    // Storage can be unavailable (private mode); the session still works in
    // memory for this page.
  }
}

function clearStored(tenantId: string): void {
  if (!tenantId || typeof sessionStorage === "undefined") return;
  try {
    sessionStorage.removeItem(storageKey(tenantId));
  } catch {
    // Nothing to clear.
  }
}

export function CallSessionProvider({ children }: { children: ReactNode }) {
  const tenantId = useSession().access?.tenantId ?? "";
  const queryClient = useQueryClient();
  const hangup = useHangupCall();
  const [session, setSession] = useState<CallSession | null>(() => readStored(tenantId));
  // A tenant change replaces the stored session without an effect.
  const [sessionTenant, setSessionTenant] = useState(tenantId);
  if (sessionTenant !== tenantId) {
    setSessionTenant(tenantId);
    setSession(readStored(tenantId));
  }
  const [actionError, setActionError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // lastState invalidates Call history when the call changes state, without
  // doing it on every poll.
  const lastState = useRef<string | null>(null);

  const query = useCallSnapshot(session?.callId ?? null);

  // A call the server no longer knows (an old stored id) is forgotten.
  // The state itself is derived below, so this only touches storage.
  useEffect(() => {
    if (query.error?.kind === "not-found") clearStored(tenantId);
  }, [query.error, tenantId]);

  useEffect(() => {
    const state = query.data?.state;
    if (!state) return;
    if (lastState.current !== state) {
      lastState.current = state;
      void queryClient.invalidateQueries({ queryKey: callKeys.all(tenantId) });
    }
    if (state === "Terminated") clearStored(tenantId);
  }, [query.data?.state, queryClient, tenantId]);

  const start = useCallback(
    (next: CallSession) => {
      setActionError(null);
      setSession(next);
      writeStored(tenantId, next);
    },
    [tenantId],
  );

  const dismiss = useCallback(() => {
    clearStored(tenantId);
    setSession(null);
    setActionError(null);
    lastState.current = null;
  }, [tenantId]);

  const end = useCallback(async () => {
    if (!session) return;
    setBusy(true);
    setActionError(null);
    try {
      await hangup.mutateAsync(session.callId);
    } catch (err) {
      setActionError(err instanceof Error ? err.message : "The call could not be ended. Try again.");
    } finally {
      setBusy(false);
    }
  }, [hangup, session]);

  // A call the server has no record of is hidden without a synchronous
  // state write from an effect.
  const visibleSession = query.error?.kind === "not-found" ? null : session;
  const phase = useMemo<SessionPhase>(() => {
    if (!visibleSession) return "starting";
    if (query.data?.state === "Terminated") return "ended";
    if (query.data?.state === "Terminating") return "ending";
    if (query.data?.state === "Active" || query.data?.state === "Degraded") return "connected";
    if (query.data?.state === "Presenting") {
      return query.data.callerAnswered ? "calling-destination" : "ringing-caller";
    }
    return "starting";
  }, [query.data, visibleSession]);

  const value: CallSessionApi = {
    session: visibleSession,
    phase,
    snapshot: query.data ?? null,
    reconnecting: visibleSession !== null && query.isError,
    actionError,
    busy,
    start,
    end,
    dismiss,
  };
  return <SessionContext.Provider value={value}>{children}</SessionContext.Provider>;
}

export function useCallSession(): CallSessionApi {
  const api = useContext(SessionContext);
  if (!api) throw new Error("useCallSession must be used inside CallSessionProvider");
  return api;
}

// reasonText names an ended call in console words. It never shows raw
// server or engine text: an unknown reason is withheld.
export function reasonText(reason: string): string {
  switch (reason) {
    case "caller_no_answer":
      return "did not answer";
    case "normal clearing":
      return "normal clearing";
    case "api_hangup":
      return "ended from the console";
    case "destination unreachable":
      return "could not be reached";
    case "call setup did not complete":
      return "setup did not complete";
    default:
      return serverText(reason);
  }
}

// formatDuration writes a call duration as m:ss (or h:mm:ss).
export function formatDuration(seconds: number): string {
  if (seconds <= 0) return "0:00";
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  if (h > 0) return `${h}:${String(m).padStart(2, "0")}:${String(s).padStart(2, "0")}`;
  return `${m}:${String(s).padStart(2, "0")}`;
}
