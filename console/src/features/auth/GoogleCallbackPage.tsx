import { useEffect, useRef, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { toApiError } from "../../api/errors";
import { Alert } from "../../components/Alert";
import { useSession, useSessionController } from "./adapter";
import { AuthLayout } from "./AuthLayout";
import { WRONG_CREDENTIALS } from "./copy";

// GoogleCallbackPage finishes "Sign in with Google" (D-67). Google sends
// the browser here with code and state. A signed-in person is linking an
// account; everyone else is signing in. The server decides which is valid
// from the state it issued.
export function GoogleCallbackPage() {
  const [params] = useSearchParams();
  const controller = useSessionController();
  const session = useSession();
  const navigate = useNavigate();
  const [failure, setFailure] = useState<string | null>(null);
  const started = useRef(false);
  const code = params.get("code") ?? "";
  const state = params.get("state") ?? "";
  const oauthError = params.get("error");
  const early =
    oauthError !== null
      ? oauthError === "access_denied"
        ? "Google sign-in was cancelled."
        : "Google could not complete the sign-in."
      : !code || !state
        ? "This sign-in link is incomplete. Start again from the sign-in page."
        : null;

  useEffect(() => {
    if (early !== null || started.current || session.status === "restoring") return;
    started.current = true;
    void (async () => {
      try {
        if (session.status === "signed-in") {
          await controller.completeGoogleLink(state, code);
          void navigate("/account?google=linked", { replace: true });
          return;
        }
        await controller.completeGoogle(state, code);
        void navigate("/", { replace: true });
      } catch (err) {
        const e = toApiError(err);
        setFailure(e.kind === "unauthenticated" ? WRONG_CREDENTIALS : e.message);
      }
    })();
  }, [code, controller, early, navigate, session.status, state]);

  const shown = early ?? failure;
  return (
    <AuthLayout title="Google sign-in">
      {shown ? (
        <Alert tone="danger" title={shown} focusOnMount>
          <p>
            <Link to="/sign-in">Back to sign in</Link>
          </p>
        </Alert>
      ) : (
        <p role="status">Completing Google sign-in…</p>
      )}
    </AuthLayout>
  );
}
