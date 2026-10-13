import { Code, ConnectError } from "@connectrpc/connect";

// ApiErrorKind is what the console needs to know about a failed call to
// decide what to show: never the server's message, which may name an
// internal component, and never Asterisk or database detail (console
// brief §6 rule 10).
export type ApiErrorKind =
  | "unauthenticated"
  | "forbidden"
  | "not-found"
  | "exists"
  | "conflict"
  | "invalid"
  | "throttled"
  | "unavailable"
  | "unknown";

const messages: Record<ApiErrorKind, string> = {
  unauthenticated: "Your session has ended. Sign in again to continue.",
  forbidden: "You do not have permission to do this.",
  "not-found": "This item no longer exists. It may have been removed.",
  exists: "That already exists.",
  conflict: "This cannot be done in the current state. Reload and try again.",
  invalid: "Some details are not valid. Check them and try again.",
  throttled: "Too many attempts. Wait a moment, then try again.",
  unavailable: "The server could not be reached. Try again shortly.",
  unknown: "Something went wrong. Try again, and contact your administrator if it continues.",
};

// ApiError is the only error a feature adapter lets through to a
// component. Its message is fixed console copy chosen by kind; the
// original error is deliberately not kept, so nothing raw can reach the
// page, a toast or a log.
export class ApiError extends Error {
  readonly kind: ApiErrorKind;
  // retryAfterSeconds is the server's throttle delay (Retry-After), when
  // it gave one. Respect it; never guess a shorter one.
  readonly retryAfterSeconds: number | undefined;

  constructor(kind: ApiErrorKind, retryAfterSeconds?: number) {
    super(messages[kind]);
    this.name = "ApiError";
    this.kind = kind;
    this.retryAfterSeconds = retryAfterSeconds;
  }
}

function kindOf(code: Code): ApiErrorKind {
  switch (code) {
    case Code.Unauthenticated:
      return "unauthenticated";
    case Code.PermissionDenied:
      return "forbidden";
    case Code.NotFound:
      return "not-found";
    case Code.AlreadyExists:
      return "exists";
    case Code.Aborted:
    case Code.FailedPrecondition:
      return "conflict";
    case Code.InvalidArgument:
    case Code.OutOfRange:
      return "invalid";
    case Code.ResourceExhausted:
      return "throttled";
    case Code.Unavailable:
    case Code.DeadlineExceeded:
      return "unavailable";
    default:
      return "unknown";
  }
}

// toApiError turns anything a call threw into an ApiError.
export function toApiError(err: unknown): ApiError {
  if (err instanceof ApiError) return err;
  const ce = ConnectError.from(err);
  const kind = kindOf(ce.code);
  if (kind !== "throttled") return new ApiError(kind);
  const wait = Number.parseInt(ce.metadata.get("Retry-After") ?? "", 10);
  return new ApiError(kind, Number.isFinite(wait) && wait > 0 ? wait : undefined);
}

// callApi runs one generated-client call for an adapter and converts any
// failure into an ApiError.
export async function callApi<T>(call: () => Promise<T>): Promise<T> {
  try {
    return await call();
  } catch (err) {
    throw toApiError(err);
  }
}
