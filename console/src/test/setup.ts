import "@testing-library/jest-dom/vitest";
import { cleanup, configure } from "@testing-library/react";
import { afterAll, afterEach, beforeAll } from "vitest";
import { server } from "./msw";

// findBy*/waitFor wait up to 5 s, not 1 s: under v8 coverage on a loaded
// machine a dialog's round trip through MSW can take longer than a second,
// and a flaky gate teaches people to rerun it. A real hang still fails.
configure({ asyncUtilTimeout: 5000 });

beforeAll(() => server.listen({ onUnhandledFrame: "error" }));
afterEach(() => {
  server.resetHandlers();
  cleanup();
  // A test's call session is presentation state for that test only; the
  // next test must not start with a stored call id.
  sessionStorage.clear();
  localStorage.clear();
});
afterAll(() => server.close());
