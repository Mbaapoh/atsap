import { useEffect, useState } from "react";
import type { ApiError } from "../../api/errors";

// useThrottle holds a form back for the server's Retry-After, so the
// person is not invited to try again before the server will listen.
export function useThrottle() {
  const [until, setUntil] = useState<number | null>(null);
  useEffect(() => {
    if (until === null) return undefined;
    // A wait already over releases on the next tick, through the same path.
    const timer = setTimeout(() => setUntil(null), Math.max(0, until - Date.now()));
    return () => clearTimeout(timer);
  }, [until]);
  return {
    blocked: until !== null,
    hold(err: ApiError) {
      if (err.kind === "throttled") setUntil(Date.now() + (err.retryAfterSeconds ?? 5) * 1000);
    },
  };
}
