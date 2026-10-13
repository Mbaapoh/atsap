import type { ApiError } from "../../api/errors";

// Console copy for the session screens. One neutral answer for a wrong
// password, an unknown email and a disabled account: nothing here tells
// anyone whether an account exists (CONSOLE-DESIGN "Sign in").
export const WRONG_CREDENTIALS = "The email or password is not correct.";

export function throttleMessage(err: ApiError): string {
  const s = err.retryAfterSeconds;
  if (!s) return "Too many attempts. Wait a moment, then try again.";
  if (s < 60) return `Too many attempts. Try again in ${s} seconds.`;
  const m = Math.ceil(s / 60);
  return `Too many attempts. Try again in ${m} minute${m === 1 ? "" : "s"}.`;
}
