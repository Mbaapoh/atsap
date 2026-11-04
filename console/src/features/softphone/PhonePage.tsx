import { useId, useState, type FormEvent } from "react";
import { Alert } from "../../components/Alert";
import { PageHeader } from "../../components/PageHeader";
import { TextField } from "../../components/TextField";
import { DeviceType } from "../../api/gen/atsapbx/v1/pbx_pb";
import { usePermissions } from "../auth/permissions";
import { useExtensionDirectory, useRegistrar, type ExtensionEntry } from "../extensions/adapter";
import { useSoftphone } from "./SoftphoneProvider";
import { DIALLED } from "../calls/copy";

// The browser phone page (D-81). An administrator chooses a WEBRTC
// extension and enters its one-time SIP password; the password is held in
// this form and then by SIP.js, never in storage or a URL.

function extensionLabel(e: ExtensionEntry): string {
  return e.name ? `${e.number} · ${e.name}` : e.number;
}

export function PhonePage() {
  const { canAdminister } = usePermissions();
  const phone = useSoftphone();
  const registrar = useRegistrar();
  const directory = useExtensionDirectory(canAdminister);
  if (!canAdminister) {
    return (
      <section className="page" aria-labelledby="page-title">
        <PageHeader title="Browser phone" description="Make and answer calls in this browser." />
        <Alert tone="info" title="Your role cannot use the browser phone">
          <p>A tenant administrator can use the browser phone in Phase A.</p>
        </Alert>
      </section>
    );
  }

  const extensions = (directory.data ?? []).filter((e) => e.device === DeviceType.WEBRTC);
  return (
    <section className="page" aria-labelledby="page-title">
      <PageHeader title="Browser phone" description="Register a WEBRTC extension in this browser and call from it." />
      {phone.registration === "registered" || phone.registration === "connecting" ? (
        <Connected phone={phone} extensions={extensions} />
      ) : (
        <ConnectForm
          extensions={extensions}
          webrtcUrl={registrar.data?.webrtcUrl ?? ""}
          domain={registrar.data?.host ?? ""}
          iceServers={registrar.data?.iceServers ?? []}
          onConnect={phone.connect}
          error={phone.error}
          loading={directory.isPending || registrar.isPending}
        />
      )}
    </section>
  );
}

function ConnectForm({
  extensions,
  webrtcUrl,
  domain,
  iceServers,
  onConnect,
  error,
  loading,
}: {
  extensions: ExtensionEntry[];
  webrtcUrl: string;
  domain: string;
  iceServers: RTCIceServer[];
  onConnect: (c: { url: string; username: string; password: string; domain: string; number: string; displayName: string; iceServers: RTCIceServer[] }) => Promise<void>;
  error: string | null;
  loading: boolean;
}) {
  const selectId = useId();
  const [extensionId, setExtensionId] = useState("");
  const [password, setPassword] = useState("");
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // The directory arrives after this form mounts; fall back to the first
  // WEBRTC extension until the person chooses another.
  const chosen = extensions.find((e) => e.id === extensionId) ?? (extensionId === "" ? extensions[0] : undefined);

  async function onSubmit(ev: FormEvent) {
    ev.preventDefault();
    setFailure(null);
    if (!chosen) {
      setFailure("Choose a WEBRTC extension.");
      return;
    }
    if (!webrtcUrl) {
      setFailure("Browser calling is not configured by the installer.");
      return;
    }
    if (!password) {
      setFailure("Enter the extension's SIP password. It is shown once when the extension is created or rotated.");
      return;
    }
    setBusy(true);
    await onConnect({
      url: webrtcUrl,
      username: chosen.sipUsername,
      password,
      domain,
      number: chosen.number,
      displayName: chosen.name || chosen.number,
      iceServers,
    });
    setPassword("");
    setBusy(false);
  }

  return (
    <div className="phone-card">
      {failure ?? error ? <Alert tone="danger" title={failure ?? error ?? ""} focusOnMount /> : null}
      {!loading && extensions.length === 0 ? (
        <Alert tone="info" title="No WEBRTC extension yet">
          <p>Create an extension with device type “Browser (WebRTC)” in Extensions, then use the SIP password shown once.</p>
        </Alert>
      ) : null}
      {!loading && !webrtcUrl ? (
        <Alert tone="info" title="Browser calling is not configured">
          <p>The installer has not set the WebRTC signalling address for this installation.</p>
        </Alert>
      ) : null}
      <form onSubmit={(e) => void onSubmit(e)} noValidate>
        <div className="field">
          <label className="field-label" htmlFor={selectId}>
            WEBRTC extension
          </label>
          <select id={selectId} className="input" value={chosen?.id ?? ""} onChange={(e) => setExtensionId(e.target.value)} disabled={loading}>
            {extensions.length === 0 ? <option value="">No WEBRTC extensions</option> : null}
            {extensions.map((e) => (
              <option key={e.id} value={e.id}>
                {extensionLabel(e)}
              </option>
            ))}
          </select>
        </div>
        <TextField
          label="SIP password"
          type="password"
          autoComplete="off"
          spellCheck={false}
          hint="The password shown once when the extension was created or last rotated. It is kept in memory only."
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          required
        />
        <p className="field-hint">Signalling: {webrtcUrl || "not configured"}</p>
        <div className="phone-actions">
          <button type="submit" className="button button-primary" disabled={busy || loading || !chosen || !webrtcUrl || !password}>
            {busy ? "Connecting…" : "Connect"}
          </button>
        </div>
      </form>
    </div>
  );
}

// remoteLabel never shows an engine endpoint id: the browser sees the
// caller as sip:e_…, and only the extension directory can turn that into a
// number. An unknown remote is "Unknown caller".
function remoteLabel(remote: string | null, extensions: ExtensionEntry[]): string {
  if (!remote) return "Unknown caller";
  const match = extensions.find((e) => e.sipUsername === remote || e.number === remote);
  if (match) return extensionLabel(match);
  if (/^\+?[0-9]{1,32}$/.test(remote)) return remote;
  return "Unknown caller";
}

function Connected({ phone, extensions }: { phone: ReturnType<typeof useSoftphone>; extensions: ExtensionEntry[] }) {
  const [destination, setDestination] = useState("");
  const [numberError, setNumberError] = useState<string | undefined>();
  if (phone.registration === "connecting") {
    return <p role="status">Connecting the browser phone…</p>;
  }
  return (
    <div className="phone-card">
      <div className="phone-head">
        <p>
          <strong>Registered{phone.number ? ` as ${phone.number}` : ""}</strong>
        </p>
        <button type="button" className="button" onClick={() => void phone.disconnect()}>
          Disconnect
        </button>
      </div>
      {phone.error ? <Alert tone="danger" title={phone.error} focusOnMount /> : null}
      {phone.call === "incoming" ? (
        <div className="phone-incoming" role="status">
          <strong>Incoming call from {remoteLabel(phone.remote, extensions)}</strong>
          <div className="phone-actions">
            <button type="button" className="button button-primary" onClick={() => setTimeout(() => void phone.answer(), 0)}>
              Answer
            </button>
            <button type="button" className="button button-danger-outline" onClick={() => void phone.reject()}>
              Reject
            </button>
          </div>
        </div>
      ) : null}
      {phone.call === "in-call" || phone.call === "calling" ? (
        <div className="phone-call" role="status">
          <strong>{phone.call === "calling" ? `Calling ${remoteLabel(phone.remote, extensions)}…` : `In call with ${remoteLabel(phone.remote, extensions)}`}</strong>
          <div className="phone-actions">
            <button type="button" className="button" onClick={phone.toggleMute}>
              {phone.muted ? "Unmute" : "Mute"}
            </button>
            <button
              type="button"
              className="button button-danger"
              onClick={() => void phone.hangup()}
            >
              Hang up
            </button>
          </div>
        </div>
      ) : null}
      {phone.call === "ended" ? (
        <div className="phone-call" role="status">
          <strong>Call ended</strong>
          <button type="button" className="button" onClick={phone.dismiss}>
            Dismiss
          </button>
        </div>
      ) : null}
      {phone.call === "idle" ? (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            if (!DIALLED.test(destination.trim())) {
              setNumberError("Use digits with an optional leading +, like 1002.");
              return;
            }
            setNumberError(undefined);
            // Defer: the Call button is replaced by Hang up when the
            // session starts, and replacing a button inside its own click
            // can send the same event to the new button.
            setTimeout(() => void phone.callNumber(destination.trim()), 0);
          }}
          noValidate
        >
          <TextField label="Number to call" inputMode="tel" autoComplete="off" spellCheck={false} value={destination} onChange={(e) => setDestination(e.target.value)} error={numberError} required />
          <div className="phone-actions">
            <button type="submit" className="button button-primary" disabled={!destination.trim()}>
              Call
            </button>
          </div>
        </form>
      ) : null}
    </div>
  );
}
