import { useState, type FormEvent } from "react";
import { Alert } from "../../components/Alert";
import { TextField } from "../../components/TextField";
import { useToast } from "../../components/Toast";
import { usePermissions } from "../auth/permissions";
import { useOutboundPolicy, useOutboundPolicyChanges } from "./adapter";

function clock(minute: number): string {
  return `${String(Math.floor(minute / 60)).padStart(2, "0")}:${String(minute % 60).padStart(2, "0")}`;
}

function field(form: FormData, name: string): string {
  const value = form.get(name);
  return typeof value === "string" ? value : "";
}

function minutes(value: string): number {
  const [h, m] = value.split(":").map(Number);
  return (h || 0) * 60 + (m || 0);
}

// OutboundProtectionSection is the fraud guard (D-85, AC-08.1/08.2):
// spend caps, blocked prefixes, anomaly detection and the suspend/resume
// state. Emergency calls are never blocked by any of it.
export function OutboundProtectionSection() {
  const policy = useOutboundPolicy();
  const { canAdminister } = usePermissions();
  const { save, resume } = useOutboundPolicyChanges();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);
  const p = policy.data;

  async function onSave(ev: FormEvent<HTMLFormElement>) {
    ev.preventDefault();
    if (!p) return;
    const form = new FormData(ev.currentTarget);
    setBusy(true);
    setFailure(null);
    try {
      await save({
        dailySpendCapMicros: Number(field(form, "daily") || 0),
        monthlySpendCapMicros: Number(field(form, "monthly") || 0),
        blockedPrefixes: field(form, "prefixes").split(",").map((s) => s.trim()).filter(Boolean),
        anomalyCallThreshold: Number(field(form, "threshold") || 0),
        anomalyWindowSeconds: Number(field(form, "window") || 600),
        businessStartMinute: minutes(field(form, "start") || "08:00"),
        businessEndMinute: minutes(field(form, "end") || "18:00"),
      });
      toast.show("Outbound protection saved.");
    } catch (err) {
      setFailure(err instanceof Error ? err.message : "Outbound protection could not be saved.");
    } finally {
      setBusy(false);
    }
  }

  async function onResume() {
    setBusy(true);
    setFailure(null);
    try {
      await resume();
      toast.show("Outbound calling resumed.");
    } catch (err) {
      setFailure(err instanceof Error ? err.message : "Outbound calling could not be resumed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section aria-labelledby="outbound-protection-title" className="overview-section">
      <div className="section-header">
        <h2 id="outbound-protection-title">Outbound protection</h2>
        {canAdminister && p?.suspended ? (
          <button type="button" className="button button-primary" onClick={() => void onResume()} disabled={busy}>
            Resume outbound calling
          </button>
        ) : null}
      </div>
      {policy.isPending ? <p role="status">Loading outbound protection…</p> : null}
      {policy.isError ? (
        <Alert tone="danger" title="Outbound protection could not be loaded">
          <p>{policy.error.kind === "forbidden" ? "Your role cannot see this setting." : policy.error.message}</p>
        </Alert>
      ) : null}
      {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
      {p?.suspended ? (
        <Alert tone="danger" title="Outbound calling is suspended">
          <p>{p.suspendedReason || "An administrator suspended outbound calling."} Emergency numbers still work; calls in progress are unaffected.</p>
        </Alert>
      ) : null}
      {p ? (
        <form onSubmit={(e) => void onSave(e)}>
          <p className="field-hint">
            Caps use the same currency as route cost, in micros (1 unit = 1,000,000 micros). 0 means unlimited. Anomaly detection
            suspends outbound when this many international calls happen inside the window outside business hours; 0 disables it.
            Times are UTC.
          </p>
          <div className="field-grid">
            <TextField label="Daily spend cap (micros)" name="daily" inputMode="numeric" defaultValue={String(p.dailySpendCapMicros)} disabled={!canAdminister} />
            <TextField label="Monthly spend cap (micros)" name="monthly" inputMode="numeric" defaultValue={String(p.monthlySpendCapMicros)} disabled={!canAdminister} />
            <TextField label="Blocked prefixes" name="prefixes" defaultValue={p.blockedPrefixes.join(", ")} hint="Comma-separated, optional +; e.g. +1900, +239" disabled={!canAdminister} />
            <TextField label="Fraud calls before suspension" name="threshold" inputMode="numeric" defaultValue={String(p.anomalyCallThreshold)} disabled={!canAdminister} />
            <TextField label="Fraud window (seconds)" name="window" inputMode="numeric" defaultValue={String(p.anomalyWindowSeconds)} disabled={!canAdminister} />
            <TextField label="Business hours start (UTC)" name="start" type="time" defaultValue={clock(p.businessStartMinute)} disabled={!canAdminister} />
            <TextField label="Business hours end (UTC)" name="end" type="time" defaultValue={clock(p.businessEndMinute)} disabled={!canAdminister} />
          </div>
          {canAdminister ? (
            <div className="form-actions">
              <button type="submit" className="button button-primary" disabled={busy}>
                Save outbound protection
              </button>
            </div>
          ) : null}
        </form>
      ) : null}
    </section>
  );
}
