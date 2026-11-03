import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from "react";
import type { CallState, RegistrationState, Softphone, SoftphoneConnection } from "./client";

// The browser phone lives above the pages so its registration and an
// incoming call survive navigation (D-81). The SIP password never enters
// React state here: it is passed to the client and held only by SIP.js
// for this page.

export interface SoftphoneApi {
  registration: RegistrationState;
  call: CallState;
  remote: string | null;
  number: string | null;
  muted: boolean;
  error: string | null;
  connect: (connection: SoftphoneConnection) => Promise<void>;
  disconnect: () => Promise<void>;
  callNumber: (destination: string) => Promise<void>;
  answer: () => Promise<void>;
  reject: () => Promise<void>;
  hangup: () => Promise<void>;
  toggleMute: () => void;
  dismiss: () => void;
}

const SoftphoneContext = createContext<SoftphoneApi | null>(null);

export function SoftphoneProvider({ children, client }: { children: ReactNode; client?: Softphone }) {
  const fixed = useRef(client);
  const current = useRef<Softphone | null>(client ?? null);
  const [registration, setRegistration] = useState<RegistrationState>("idle");
  const [call, setCall] = useState<CallState>("idle");
  const [remote, setRemote] = useState<string | null>(null);
  const [number, setNumber] = useState<string | null>(null);
  const [muted, setMuted] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const bind = useCallback((phone: Softphone) => {
    phone.delegate = {
      onRegistration(state, message) {
        setRegistration(state);
        if (state === "error") setError(message ?? "The browser phone could not register.");
      },
      onCall(state, who, message) {
        setCall(state);
        if (who !== undefined) setRemote(who);
        if (state === "idle" || state === "ended") setMuted(false);
        if (message) setError(message);
      },
    };
  }, []);

  const getClient = useCallback(async (): Promise<Softphone> => {
    if (current.current) return current.current;
    // Load SIP.js only when someone actually connects.
    const { SipSoftphone } = await import("./client");
    const phone = new SipSoftphone();
    current.current = phone;
    return phone;
  }, []);

  const connect = useCallback(
    async (connection: SoftphoneConnection) => {
      setError(null);
      setRemote(null);
      try {
        const phone = await getClient();
        bind(phone);
        setNumber(connection.number ?? null);
        await phone.connect(connection);
      } catch {
        setRegistration("error");
        setError("The browser phone could not connect. Check the extension password and signalling address.");
      }
    },
    [bind, getClient],
  );

  const disconnect = useCallback(async () => {
    try {
      await current.current?.disconnect();
    } finally {
      if (!fixed.current) current.current = null;
      setRegistration("idle");
      setCall("idle");
      setRemote(null);
      setNumber(null);
      setMuted(false);
    }
  }, []);

  const callNumber = useCallback(
    async (destination: string) => {
      setError(null);
      await current.current?.call(destination);
    },
    [],
  );
  const answer = useCallback(async () => {
    await current.current?.answer();
  }, []);
  const reject = useCallback(async () => {
    await current.current?.reject();
  }, []);
  const hangup = useCallback(async () => {
    await current.current?.hangup();
  }, []);
  const toggleMute = useCallback(() => {
    setMuted((m) => {
      current.current?.mute(!m);
      return !m;
    });
  }, []);
  const dismiss = useCallback(() => {
    setCall("idle");
    setRemote(null);
    setError(null);
  }, []);

  const value = useMemo<SoftphoneApi>(
    () => ({ registration, call, remote, number, muted, error, connect, disconnect, callNumber, answer, reject, hangup, toggleMute, dismiss }),
    [registration, call, remote, number, muted, error, connect, disconnect, callNumber, answer, reject, hangup, toggleMute, dismiss],
  );
  return <SoftphoneContext.Provider value={value}>{children}</SoftphoneContext.Provider>;
}

export function useSoftphone(): SoftphoneApi {
  const api = useContext(SoftphoneContext);
  if (!api) throw new Error("useSoftphone must be used inside SoftphoneProvider");
  return api;
}
