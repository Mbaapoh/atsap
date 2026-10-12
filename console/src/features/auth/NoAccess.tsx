import { useState } from "react";
import { useSessionController } from "./adapter";
import { AuthLayout } from "./AuthLayout";

// NoAccess is a signed-in person with no active membership: nothing to
// choose, nothing to administer. Say so and offer the way out.
export function NoAccess() {
  const controller = useSessionController();
  const [busy, setBusy] = useState(false);
  return (
    <AuthLayout title="No active access">
      <p>Your account is not a member of any active tenant. Ask your administrator to invite you.</p>
      <div className="form-actions">
        <button
          type="button"
          className="button"
          disabled={busy}
          onClick={() => {
            setBusy(true);
            void controller.signOut().catch(() => undefined);
          }}
        >
          Sign out
        </button>
      </div>
    </AuthLayout>
  );
}
