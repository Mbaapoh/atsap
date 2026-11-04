import { useId, useState } from "react";
import { toApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { Modal } from "../../components/Modal";
import { useAssignablePrincipals, useExtensionChanges, type ExtensionRow } from "./adapter";

// AssignExtensionDialog links a membership to an extension (D-65). An
// extension has at most one owner; a person may own several.
export function AssignExtensionDialog({ extension, onClose }: { extension: ExtensionRow | null; onClose: () => void }) {
  if (extension === null) return null;
  return <AssignForm key={extension.id} extension={extension} onClose={onClose} />;
}

function AssignForm({ extension, onClose }: { extension: ExtensionRow; onClose: () => void }) {
  const titleId = useId();
  const selectId = useId();
  const people = useAssignablePrincipals();
  const { assign, unassign } = useExtensionChanges();
  const [principalId, setPrincipalId] = useState(extension.assignedPrincipalId);
  const [failure, setFailure] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function onSave() {
    setBusy(true);
    setFailure(null);
    try {
      if (principalId === "") await unassign(extension.id);
      else await assign({ id: extension.id, principalId });
      onClose();
    } catch (err) {
      const e = toApiError(err);
      setFailure(e.kind === "invalid" ? "Choose an active member of this tenant." : e.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal open onClose={() => (busy ? undefined : onClose())} labelledBy={titleId}>
      <h2 id={titleId} className="modal-title">
        Assign extension {extension.number}
      </h2>
      <div className="modal-body">
        {failure ? <Alert tone="danger" title={failure} focusOnMount /> : null}
        <div className="field">
          <label className="field-label" htmlFor={selectId}>
            Assigned to
          </label>
          <select id={selectId} className="input" value={principalId} onChange={(e) => setPrincipalId(e.target.value)}>
            <option value="">No owner</option>
            {(people.data ?? []).map((p) => (
              <option key={p.id} value={p.id}>
                {p.name ? `${p.name} · ${p.email}` : p.email}
              </option>
            ))}
          </select>
        </div>
        <p className="field-hint">A person may own several extensions; an extension has one owner. Unassigned lines still work.</p>
      </div>
      <div className="modal-actions">
        <button type="button" className="button" onClick={onClose} disabled={busy}>
          Cancel
        </button>
        <button type="button" className="button button-primary" onClick={() => void onSave()} disabled={busy}>
          Save assignment
        </button>
      </div>
    </Modal>
  );
}
