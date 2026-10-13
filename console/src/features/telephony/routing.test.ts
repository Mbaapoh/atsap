import { describe, expect, it } from "vitest";
import { formatCost, overlaps, parseCost, PREFIX } from "./routing";

describe("route prefixes", () => {
  it.each([
    ["+237", true], ["237", true], ["44XX", true], ["+", false], ["sip:44", false], ["44*", false], ["4 4", false],
  ])("%s valid: %s", (p, ok) => expect(PREFIX.test(p)).toBe(ok));

  it.each([
    ["+237", "+2376", true],
    ["237", "+23769", true],
    ["44X", "441", true],
    ["44", "45", false],
    ["+1", "+44", false],
    ["2X7", "247", true],
  ])("%s overlaps %s: %s", (a, b, want) => {
    expect(overlaps(a, b)).toBe(want);
    expect(overlaps(b, a)).toBe(want);
  });
});

describe("cost", () => {
  it.each([
    ["", 0n], ["0", 0n], ["1", 1_000_000n], ["0.015", 15_000n], ["2.000001", 2_000_001n],
  ])("%j parses to %s micros", (t, m) => expect(parseCost(t)).toBe(m));

  it.each(["-1", "1.0000001", "abc", "1,5"])("%j is refused", (t) => expect(parseCost(t)).toBeNull());

  it("formats without trailing zeros", () => {
    expect(formatCost(15_000n)).toBe("0.015");
    expect(formatCost(2_000_000n)).toBe("2");
  });
});
