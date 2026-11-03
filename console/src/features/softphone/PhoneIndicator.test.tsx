import { describe, expect, it } from "vitest";
import { indicatorLabel } from "./PhoneIndicator";

describe("browser phone indicator", () => {
  const base = { registration: "idle" as const, call: "idle" as const, number: null };
  it("names every state", () => {
    expect(indicatorLabel(base)).toBe("Browser phone");
    expect(indicatorLabel({ ...base, registration: "connecting" })).toBe("Phone: connecting…");
    expect(indicatorLabel({ ...base, registration: "error" })).toBe("Phone: unavailable");
    expect(indicatorLabel({ ...base, registration: "registered", number: "1001" })).toBe("Phone 1001");
    expect(indicatorLabel({ ...base, registration: "registered" })).toBe("Browser phone");
    expect(indicatorLabel({ ...base, registration: "registered", call: "incoming" })).toBe("Incoming call");
    expect(indicatorLabel({ ...base, registration: "registered", call: "in-call" })).toBe("Phone: in call");
    expect(indicatorLabel({ ...base, registration: "registered", call: "calling" })).toBe("Phone: calling…");
  });
});
