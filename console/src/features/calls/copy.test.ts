import { describe, expect, it } from "vitest";
import { ApiError } from "../../api/errors";
import { hangupText } from "./NewCall";
import { formatDuration, reasonText } from "./session";

// console-call-session: the fixed words behind the session bar and the
// dialer, tested without rendering.

describe("call duration", () => {
  it.each([
    [0, "0:00"],
    [59, "0:59"],
    [65, "1:05"],
    [3661, "1:01:01"],
  ])("formats %i seconds as %s", (seconds, text) => {
    expect(formatDuration(seconds)).toBe(text);
  });
});

describe("ending reasons", () => {
  it.each([
    ["caller_no_answer", "did not answer"],
    ["normal clearing", "normal clearing"],
    ["api_hangup", "ended from the console"],
    ["destination unreachable", "could not be reached"],
    ["call setup did not complete", "setup did not complete"],
    ["mystery", "mystery"],
  ])("maps %j to %j", (reason, text) => {
    expect(reasonText(reason)).toBe(text);
  });

  it("withholds engine vocabulary", () => {
    expect(reasonText("hangup by PJSIP/e_0123456789abcdef0123456789abcdef")).toBe("Not shown");
  });
});

describe("hang-up failures", () => {
  it("maps known kinds and defaults to a retry", () => {
    expect(hangupText(new ApiError("forbidden"))).toBe("Your role cannot end this call.");
    expect(hangupText(new ApiError("not-found"))).toBe("This call no longer exists. Reload the list.");
    expect(hangupText(new ApiError("unavailable"))).toBe("The call could not be ended. Try again.");
  });
});
