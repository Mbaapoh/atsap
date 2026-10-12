import { useEffect, useState } from "react";
import type { ApiError } from "../../api/errors";

// useThrottle holds a form back for the server's Retry-After, so the
// person is not invited to try again before the server will listen.
export function useThrottle() {
  const [until, setUntil] = useState<number | null>(null);
  useEffect(() => {
    if (until === null) return;
    const left = until - Date.now();
    if (left <= 0) {
      setUntil(null);
      return;
    }
    const timer = setTimeout(() => setUntil(null), left);
    return () => clearTimeout(timer);
  }, [until]);
  return {
    blocked: until !== null,
    hold(err: ApiError) {
      if (err.kind === "throttled") setUntil(Date.now() + (err.retryAfterSeconds ?? 5) * 1000);
    },
  };
}
