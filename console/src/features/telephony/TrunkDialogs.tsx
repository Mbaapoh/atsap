import { useEffect, useId, useState, type FormEvent } from "react";
import { toApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { ConfirmDialog } from "../../components/ConfirmDialog";
import { Modal } from "../../components/Modal";
import { TextField } from "../../components/TextField";
import { useToast } from "../../components/Toast";
import { useTrunkChanges, type TrunkInput, type TrunkRow } from "./adapter";

// Field rules mirror the server's (pbx/domain/trunk.go); the server still
// decides. A host is a bare hostname or IP: no scheme, port or user part.
const HOST = /^(?=.{1,253}$)[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$|^[0-9a-fA-F:]+$/;
const USERNAME = /^[A-Za-z0-9_.+-]{0,100}$/;

interface Draft {
  name: string;
  host: string;
  port: string;
  authUsername: string;
  secret: string;
  channelLimit: string;
  priority: string;
}

const empty: Draft = { name: "", host: "", port: "", authUsername: "", secret: "", channelLimit: "", priority: "0" };

function validate(d: Draft): { errors: Partial<Record<keyof Draft, string>>; input?: TrunkInput } {
  const errors: Partial<Record<keyof Draft, string>> = {};
  const name = d.name.trim();
  if (!name) errors.name = "Enter a name, like Carrier A.";
  else if ([...name].length > 100) errors.name = "Use at most 100 characters.";
  if (!HOST.test(d.host.trim())) errors.host = "Enter a hostname or IP address only, like sip.carrier.example — no sip:, port or user.";
  const port = d.port.trim() === "" ? 0 : Number(d.port);
  if (!Number.isInteger(port) || port < 0 || port > 65535) errors.port = "Use a port from 1 to 65535, or leave empty for 5060.";
  const limit = Number(d.channelLimit);
  if (!Number.isInteger(limit) || limit < 1) errors.channelLimit = "Enter how many calls this trunk may carry at once (1 or more).";
  const priority = Number(d.priority);
  if (!Number.isInteger(priority) || priority < 0) errors.priority = "Use 0 or more; lower is preferred.";
  if (!USERNAME.test(d.authUsername.trim())) errors.authUsername = "Use letters, digits and - _ . + only.";
  if (d.secret && !d.authUsername.trim()) errors.secret = "A secret needs a username.";
  if ([...d.secret].length > 255) errors.secret = "Use at most 255 characters.";
  if (Object.keys(errors).length) return { errors };
  return {
    errors,
    input: { name, host: d.host.trim(), port, authUsername: d.authUsername.trim(), secret: d.secret, channelLimit: limit, priority },
  };
}

// TrunkDialog creates a trunk, or edits one. The carrier secret is
// write-only: it is never shown, and on edit an empty field keeps it.
export function TrunkDialog({ trunk, open, onClose }: { trunk: TrunkRow | null; open: boolean; onClose: () => void }) {
  const titleId = useId();
  const { create, update } = useTrunkChanges();
  const toast = useToast();
  const [draft, setDraft] = useState<Draft>(empty);
  const [errors, setErrors] = useState<Partial<Record<keyof Draft, string>>>({});
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const editing = trunk !== null;

  useEffect(() => {
    if (!open) return;
    setDraft(
      trunk
        ? { name: trunk.name, host: trunk.host, port: trunk.port ? String(trunk.port) : "", authUsername: trunk.authUsername, secret: "", channelLimit: String(trunk.channelLimit), priority: String(trunk.priority) }
        : empty,
    );
    setErrors({});
    setFailure(null);
  }, [open, trunk]);

  const set = (k: keyof Draft) => (e: { target: { value: string } }) => setDraft((d) => ({ ...d, [k]: e.target.value }));

  async function onSubmit(ev: FormEvent) {
    ev.preventDefault();
    setFailure(null);
    const { errors: found, input } = validate(draft);
    setErrors(found);
    if (!input) return;
    setBusy(true);
    try {
      if (trunk) await update({ id: trunk.id, input });
      else await create(input);
      toast.show(editing ? `Trunk ${input.name} updated.` : `Trunk ${input.name} added.`);
      onClose();
    } catch (err) {
      const e = toApiError(err);
      setFailure(
        e.kind === "invalid"
          ? "Some details are not valid. Check the host, port and username."
          : e.kind === "exists"
            ? "A trunk with that name already exists."
            : e.kind === "forbidden"
              ? "Your role cannot change trunks."
              : e.message,
      );
    } finally {
      setDraft((d) => ({ ...d, secret: "" }));
      setBusy(false);
    }
  }

  return (
    <Modal open={open} onClose={() => (busy ? undefined : onClose())} labelledBy={titleId}>
      <h2 id={titleId} className="modal-title">
        {editing ? `Edit trunk ${trunk?.name}` : "Add a trunk"}
      </h2>
      <form onSubmit={onSubmit} noValidate>
        <div className="modal-body">
          {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
          <TextField label="Name" autoComplete="off" value={draft.name} onChange={set("name")} error={errors.name} required />
          <TextField label="Host" autoComplete="off" spellCheck={false} hint="The carrier's SIP hostname or IP address." value={draft.host} onChange={set("host")} error={errors.host} required />
          <TextField label="Port (optional)" inputMode="numeric" autoComplete="off" hint="Leave empty for the SIP default, 5060." value={draft.port} onChange={set("port")} error={errors.port} />
          <TextField label="Channel limit" inputMode="numeric" autoComplete="off" hint="Calls this trunk may carry at once." value={draft.channelLimit} onChange={set("channelLimit")} error={errors.channelLimit} required />
          <TextField label="Priority" inputMode="numeric" autoComplete="off" hint="Lower is preferred when routes tie." value={draft.priority} onChange={set("priority")} error={errors.priority} required />
          <TextField label="Username (optional)" autoComplete="off" spellCheck={false} hint="Leave empty for a carrier that recognises you by IP address." value={draft.authUsername} onChange={set("authUsername")} error={errors.authUsername} />
          <TextField
            label="Secret (optional)"
            type="password"
            autoComplete="new-password"
            hint={editing ? (trunk?.hasSecret ? "A secret is stored. Leave empty to keep it; type a new one to replace it." : "No secret is stored.") : "The carrier's password, if it uses one. It is never shown again."}
            value={draft.secret}
            onChange={set("secret")}
            error={errors.secret}
          />
        </div>
        <div className="modal-actions">
          <button type="button" className="button" onClick={onClose} disabled={busy}>
            Cancel
          </button>
          <button type="submit" className="button button-primary" disabled={busy}>
            {editing ? "Save changes" : "Add trunk"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

export function DeleteTrunkDialog({ trunk, onClose }: { trunk: TrunkRow | null; onClose: () => void }) {
  const { remove } = useTrunkChanges();
  const toast = useToast();
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<string | null>(null);

  async function onConfirm() {
    if (!trunk) return;
    setBusy(true);
    setFailure(null);
    try {
      await remove(trunk.id);
      toast.show(`Trunk ${trunk.name} deleted.`);
      onClose();
    } catch (err) {
      const e = toApiError(err);
      setFailure(e.kind === "forbidden" ? "Your role cannot delete trunks." : e.kind === "not-found" ? "This trunk no longer exists." : e.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <ConfirmDialog
      open={trunk !== null}
      title={`Delete trunk ${trunk?.name ?? ""}?`}
      confirmLabel="Delete trunk"
      destructive
      busy={busy}
      onConfirm={() => void onConfirm()}
      onCancel={() => {
        setFailure(null);
        onClose();
      }}
    >
      {failure ? (
        <Alert tone="danger" title={failure} focusOnMount />
      ) : (
        <p>Every outbound route that uses it is deleted too. Calls already in progress continue; no new call will use it.</p>
      )}
    </ConfirmDialog>
  );
}
