import { Invitation, Inviter, Registerer, RegistererState, SessionState, UserAgent, Web } from "sip.js";

// The browser phone's SIP client (console-browser-phone, D-81). SIP.js is
// imported only by the softphone route; the provider talks to this small
// interface, so the UI is testable with a fake and no engine.

export interface SoftphoneConnection {
  // url is the WebRTC signalling endpoint: ws:// on the dev rig, wss:// in
  // production.
  url: string;
  // username is the extension's SIP username; domain is the registrar
  // host used in the SIP URI.
  username: string;
  password: string;
  domain: string;
  displayName?: string;
  // number is the extension's number, for display only.
  number?: string;
}

export type RegistrationState = "idle" | "connecting" | "registered" | "error";
export type CallState = "idle" | "calling" | "incoming" | "in-call" | "ended";

export interface SoftphoneDelegate {
  onRegistration(state: RegistrationState, error?: string): void;
  onCall(state: CallState, remote?: string, error?: string): void;
}

export interface Softphone {
  delegate: SoftphoneDelegate;
  connect(connection: SoftphoneConnection): Promise<void>;
  disconnect(): Promise<void>;
  call(destination: string): Promise<void>;
  answer(): Promise<void>;
  reject(): Promise<void>;
  hangup(): Promise<void>;
  mute(muted: boolean): void;
}

// errorText is the fixed console copy for a SIP/WebRTC failure. The raw
// error may name an engine or a URL; it never reaches the page.
function errorText(action: string): string {
  return `${action} failed. Check the extension password, the signalling address and the microphone, then try again.`;
}

export class SipSoftphone implements Softphone {
  delegate: SoftphoneDelegate = { onRegistration() {}, onCall() {} };
  private ua: UserAgent | null = null;
  private registerer: Registerer | null = null;
  private session: Inviter | Invitation | null = null;
  private audio: HTMLAudioElement | null = null;
  private domain = "";

  async connect(c: SoftphoneConnection): Promise<void> {
    await this.disconnect();
    this.domain = c.domain;
    const uri = UserAgent.makeURI(`sip:${c.username}@${c.domain}`);
    if (!uri) {
      this.delegate.onRegistration("error", errorText("Connecting"));
      return;
    }
    this.delegate.onRegistration("connecting");
    this.audio = document.createElement("audio");
    this.audio.autoplay = true;
    this.audio.hidden = true;
    document.body.appendChild(this.audio);

    const ua = new UserAgent({
      uri,
      transportOptions: { server: c.url },
      authorizationUsername: c.username,
      authorizationPassword: c.password,
      displayName: c.displayName ?? c.username,
      contactParams: { transport: c.url.startsWith("wss") ? "wss" : "ws" },
      // Phase A is host candidates only; a production NAT needs TURN,
      // recorded as a deployment gap (D-81).
      sessionDescriptionHandlerFactoryOptions: { peerConnectionConfiguration: { iceServers: [] } },
      delegate: { onInvite: (invitation) => this.onInvite(invitation) },
    });
    this.ua = ua;
    try {
      await ua.start();
      const registerer = new Registerer(ua);
      this.registerer = registerer;
      registerer.stateChange.addListener((state) => {
        if (state === RegistererState.Registered) this.delegate.onRegistration("registered");
        if (state === RegistererState.Unregistered || state === RegistererState.Terminated) this.delegate.onRegistration("idle");
      });
      await registerer.register();
    } catch {
      this.delegate.onRegistration("error", errorText("Connecting"));
      await this.disconnect();
    }
  }

  async disconnect(): Promise<void> {
    try {
      await this.session?.dispose();
    } catch {
      // Already gone.
    }
    this.session = null;
    try {
      await this.registerer?.unregister();
    } catch {
      // Best effort.
    }
    this.registerer = null;
    try {
      await this.ua?.stop();
    } catch {
      // Already stopped.
    }
    this.ua = null;
    if (this.audio) {
      this.audio.srcObject = null;
      this.audio.remove();
      this.audio = null;
    }
    this.delegate.onRegistration("idle");
    this.delegate.onCall("idle");
  }

  async call(destination: string): Promise<void> {
    if (!this.ua || this.session) throw new Error("not ready");
    const target = UserAgent.makeURI(`sip:${destination}@${this.domain}`);
    if (!target) {
      this.delegate.onCall("idle", undefined, errorText("The call"));
      return;
    }
    const inviter = new Inviter(this.ua, target, {
      sessionDescriptionHandlerOptions: { constraints: { audio: true, video: false } },
    });
    this.session = inviter;
    this.watch(inviter, destination);
    this.delegate.onCall("calling", destination);
    try {
      await inviter.invite();
    } catch {
      if (this.session === inviter) this.session = null;
      this.delegate.onCall("idle", undefined, errorText("The call"));
    }
  }

  async answer(): Promise<void> {
    if (!(this.session instanceof Invitation)) return;
    try {
      await this.session.accept({ sessionDescriptionHandlerOptions: { constraints: { audio: true, video: false } } });
    } catch {
      this.delegate.onCall("idle", undefined, errorText("Answering"));
    }
  }

  async reject(): Promise<void> {
    if (this.session instanceof Invitation) {
      try {
        await this.session.reject();
      } catch {
        // The caller may have gone.
      }
      this.session = null;
      this.delegate.onCall("idle");
    }
  }

  async hangup(): Promise<void> {
    const session = this.session;
    if (!session) return;
    this.session = null;
    try {
      if (session instanceof Invitation) {
        await session.reject();
      } else if (session.state === SessionState.Established) {
        await session.bye();
      } else {
        await session.cancel();
      }
    } catch {
      // The far end may have hung up first.
    }
    this.delegate.onCall("ended");
  }

  mute(muted: boolean): void {
    const session = this.session;
    const sdh = session?.sessionDescriptionHandler;
    if (sdh instanceof Web.SessionDescriptionHandler) {
      const pc = sdh.peerConnection;
      for (const sender of pc?.getSenders() ?? []) {
        if (sender.track?.kind === "audio") sender.track.enabled = !muted;
      }
    }
  }

  private onInvite(invitation: Invitation): void {
    if (this.session) {
      void invitation.reject();
      return;
    }
    this.session = invitation;
    const remote = invitation.remoteIdentity.uri.user || invitation.remoteIdentity.displayName || "unknown";
    this.watch(invitation, remote);
    this.delegate.onCall("incoming", remote);
  }

  private watch(session: Inviter | Invitation, remote: string): void {
    session.stateChange.addListener((state) => {
      if (state === SessionState.Establishing) this.attachMedia(session);
      if (state === SessionState.Established) {
        this.attachMedia(session);
        this.delegate.onCall("in-call", remote);
      }
      if (state === SessionState.Terminated) {
        if (this.session === session) this.session = null;
        if (this.audio) this.audio.srcObject = null;
        this.delegate.onCall("ended", remote);
      }
    });
  }

  private attachMedia(session: Inviter | Invitation): void {
    const sdh = session.sessionDescriptionHandler;
    if (!(sdh instanceof Web.SessionDescriptionHandler)) return;
    const pc = sdh.peerConnection;
    if (!pc || !this.audio) return;
    pc.ontrack = (event) => this.play(event.streams[0] ?? new MediaStream([event.track]));
    const live = pc.getReceivers().map((r) => r.track).filter((track): track is MediaStreamTrack => Boolean(track));
    if (live.length > 0) this.play(new MediaStream(live));
  }

  private play(stream: MediaStream): void {
    if (!this.audio) return;
    this.audio.srcObject = stream;
    void this.audio.play().catch(() => {
      // Autoplay can be refused before a gesture; the Connect/Call click
      // is the gesture, so this is only a best-effort retry.
    });
  }
}
