import { useId, useState, type FormEvent } from "react";
import { toApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Modal } from "../../components/Modal";
import { TextField } from "../../components/TextField";
import { useToast } from "../../components/Toast";
import { useRouteChanges, type RouteInput, type RouteRow, type TrunkRow } from "./adapter";
import { formatCost, overlaps, parseCost, PREFIX } from "./routing";

function failureText(err: unknown): string {
  const e = toApiError(err);
  if (e.kind === "forbidden") return "Your role cannot change routes.";
  if (e.kind === "invalid") return "Some details are not valid. Check the prefix, trunk and cost.";
  if (e.kind === "not-found") return "That trunk or route no longer exists. Reload and try again.";
  return e.message;
}

// RouteDialog creates a route, or prepares a replacement for one. Overlap
// with an existing route is allowed — it is how least-cost preference is
// expressed — so it is explained, never blocked (CONSOLE-DESIGN).
type RouteDialogProps = { open: boolean; replacing: RouteRow | null; routes: RouteRow[]; trunks: TrunkRow[]; onClose: () => void };

// The form is mounted only while open, and anew for each route replaced,
// so every opening starts from its own values with no reset effect.
export function RouteDialog(props: RouteDialogProps) {
  if (!props.open) return null;
  return <RouteForm key={props.replacing?.id ?? "new"} {...props} />;
}

function RouteForm({ open, replacing, routes, trunks, onClose }: RouteDialogProps) {
  const titleId = useId();
  const trunkId = useId();
  const { create, remove } = useRouteChanges();
  const toast = useToast();
  const [prefix, setPrefix] = useState(replacing?.prefix ?? "");
  const [trunk, setTrunk] = useState(replacing?.trunkId ?? trunks[0]?.id ?? "");
  const [cost, setCost] = useState(replacing ? formatCost(replacing.costMicros) : "");
  const [priority, setPriority] = useState(replacing ? String(replacing.priority) : "0");
  const [errors, setErrors] = useState<{ prefix?: string; trunk?: string; cost?: string; priority?: string }>({});
  const [failure, setFailure] = useState<string | null>(null);
  const [confirming, setConfirming] = useState<RouteInput | null>(null);
  const [busy, setBusy] = useState(false);

  const trunkName = (id: string) => trunks.find((t) => t.id === id)?.name ?? "another trunk";
  const overlapping = PREFIX.test(prefix.trim()) ? routes.filter((r) => r.id !== replacing?.id && overlaps(r.prefix, prefix.trim())) : [];

  function validate(): RouteInput | null {
    const found: typeof errors = {};
    const p = prefix.trim();
    if (!PREFIX.test(p)) found.prefix = "Use digits and X (any one digit), with an optional leading +, like +237 or 44XX.";
    if (!trunk) found.trunk = "Choose the trunk these calls use.";
    const micros = parseCost(cost);
    if (micros === null) found.cost = "Enter a cost like 0.015 (up to 6 decimals), or leave empty for 0.";
    const prio = Number(priority);
    if (!Number.isInteger(prio) || prio < 0) found.priority = "Use 0 or more; lower is preferred.";
    setErrors(found);
    if (Object.keys(found).length || micros === null) return null;
    return { prefix: p, trunkId: trunk, costMicros: micros, priority: prio };
  }

  async function apply(input: RouteInput) {
    setBusy(true);
    setFailure(null);
    try {
      await create(input);
      if (replacing) {
        try {
          await remove(replacing.id);
        } catch (err) {
          setConfirming(null);
          setFailure(`The new route was added, but the old one could not be deleted: ${failureText(err)} Delete it from the list.`);
          return;
        }
      }
      toast.show(replacing ? `Route ${input.prefix} replaced.` : `Route ${input.prefix} added.`);
      setConfirming(null);
      onClose();
    } catch (err) {
      setConfirming(null);
      setFailure(failureText(err));
    } finally {
      setBusy(false);
    }
  }

  function onSubmit(ev: FormEvent) {
    ev.preventDefault();
    const input = validate();
    if (!input) return;
    if (replacing) setConfirming(input);
    else void apply(input);
  }

  return (
    <>
      <Modal open={open && confirming === null} onClose={() => (busy ? undefined : onClose())} labelledBy={titleId}>
        <h2 id={titleId} className="modal-title">
          {replacing ? `Replace route ${replacing.prefix}` : "Add an outbound route"}
        </h2>
        <form onSubmit={onSubmit} noValidate>
          <div className="modal-body">
            {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
            <TextField label="Prefix" autoComplete="off" spellCheck={false} hint="Dialled numbers starting with this use the route. X matches any one digit." value={prefix} onChange={(e) => setPrefix(e.target.value.trim())} error={errors.prefix} required />
            <div className="field">
              <label className="field-label" htmlFor={trunkId}>
                Trunk
              </label>
              <select id={trunkId} className="input" value={trunk} onChange={(e) => setTrunk(e.target.value)} aria-invalid={errors.trunk ? true : undefined}>
                {trunks.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.name}
                  </option>
                ))}
              </select>
              {errors.trunk ? <p className="field-error">{errors.trunk}</p> : null}
            </div>
            <TextField label="Cost per minute (optional)" inputMode="decimal" autoComplete="off" hint="Stored for least-cost ordering; the platform never computes a price from it." value={cost} onChange={(e) => setCost(e.target.value)} error={errors.cost} />
            <TextField label="Priority" inputMode="numeric" autoComplete="off" hint="Lower is preferred when prefixes are equally long." value={priority} onChange={(e) => setPriority(e.target.value)} error={errors.priority} required />
            {overlapping.length ? (
              <Alert tone="info" title="This prefix overlaps other routes">
                <p>
                  It overlaps {overlapping.map((r) => `${r.prefix} (${trunkName(r.trunkId)})`).join(", ")}. That is allowed. For each call, routes on a healthy trunk are
                  tried before those on a degraded one, and an unhealthy trunk only when no other can take the call; then the longest matching prefix comes first, then the lower priority, then the lower stored cost.
                </p>
              </Alert>
            ) : null}
          </div>
          <div className="modal-actions">
            <button type="button" className="button" onClick={onClose} disabled={busy}>
              Cancel
            </button>
            <button type="submit" className="button button-primary" disabled={busy}>
              {replacing ? "Continue" : "Add route"}
            </button>
          </div>
        </form>
      </Modal>
      <ConfirmDialog
        open={confirming !== null}
        title={`Replace route ${replacing?.prefix ?? ""}?`}
        confirmLabel="Replace route"
        destructive
        busy={busy}
        onConfirm={() => confirming && void apply(confirming)}
        onCancel={() => setConfirming(null)}
      >
        <p>
          The new route ({confirming?.prefix} via {confirming ? trunkName(confirming.trunkId) : ""}) is added first, then the old one is deleted, so calls are never
          left without a route.
        </p>
      </ConfirmDialog>
    </>
  );
}

export function DeleteRouteDialog({ route, trunkName, onClose }: { route: RouteRow | null; trunkName: string; onClose: () => void }) {
  const { remove } = useRouteChanges();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);

  async function onConfirm() {
    if (!route) return;
    setBusy(true);
    setFailure(null);
    try {
      await remove(route.id);
      toast.show(`Route ${route.prefix} deleted.`);
      onClose();
    } catch (err) {
      setFailure(failureText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <ConfirmDialog
      open={route !== null}
      title={`Delete route ${route?.prefix ?? ""}?`}
      confirmLabel="Delete route"
      destructive
      busy={busy}
      onConfirm={() => void onConfirm()}
      onCancel={() => {
        setFailure(null);
        onClose();
      }}
    >
      {failure ? <Alert tone="danger" title={failure} focusOnMount /> : <p>Calls to {route?.prefix} no longer use {trunkName}; if no other route matches them, they cannot be placed.</p>}
    </ConfirmDialog>
  );
}
