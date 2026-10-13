import { describe, expect, it } from "vitest";
import { serverText, WITHHELD } from "./copy";
import { CALL_STATES, localMidnight, range, readFilters, writeFilters } from "./filters";

describe("call filters in the address", () => {
  it("read back what was written", () => {
    const f = { from: "2026-10-01", to: "2026-10-12", direction: "INBOUND", state: "Terminated", page: 2 };
    expect(writeFilters(f).toString()).toBe("from=2026-10-01&to=2026-10-12&direction=INBOUND&state=Terminated&page=3");
    expect(readFilters(writeFilters(f))).toEqual(f);
  });

  it("drop what they do not recognise rather than send it", () => {
    expect(readFilters(new URLSearchParams("from=yesterday&to=2026-13-45&direction=SIDEWAYS&state=Answered&page=-1"))).toEqual({
      from: "",
      to: "",
      direction: "",
      state: "",
      page: 0,
    });
    for (const s of ["Answered", "Missed", "Failed"]) expect(readFilters(new URLSearchParams(`state=${s}`)).state).toBe("");
  });

  it("offer the lifecycle states exactly as the server names them", () => {
    expect([...CALL_STATES]).toEqual(["Initiated", "Screening", "Routing", "Presenting", "Active", "Degraded", "Terminating", "Terminated"]);
  });

  it("send whole local days, the end day included, as a half-open range", () => {
    const { from, to } = range({ from: "2026-10-01", to: "2026-10-12", direction: "", state: "", page: 0 });
    expect(from).toEqual(localMidnight("2026-10-01"));
    expect(to).toEqual(localMidnight("2026-10-13"));
    expect(range({ from: "", to: "", direction: "", state: "", page: 0 })).toEqual({});
  });
});

describe("server text", () => {
  it.each([
    ["normal clearing", "normal clearing"],
    ["capacity verdict rejected the call", "capacity verdict rejected the call"],
    ["hangup by PJSIP/e_0123456789abcdef0123456789abcdef", WITHHELD],
    ["channel 1791844407.123 gone", WITHHELD],
    ["bridge-4f9c2a1e-77aa destroyed", WITHHELD],
    ["ps_endpoints row missing", WITHHELD],
    ["see pjsip.conf", WITHHELD],
    ["stasis-in", WITHHELD],
    ["Asterisk said no", WITHHELD],
  ])("%s → %s", (text, shown) => {
    expect(serverText(text)).toBe(shown);
  });
});
