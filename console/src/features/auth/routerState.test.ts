import { describe, expect, it } from "vitest";
import { returnPath, stateString } from "./routerState";

describe("router state", () => {
  it.each([
    [{ from: "/calls?page=2" }, "/calls?page=2"],
    [{ from: "//evil.example/steal" }, "/"],
    [{ from: "https://evil.example" }, "/"],
    [{ from: 42 }, "/"],
    [null, "/"],
    ["/calls", "/"],
    [undefined, "/"],
  ])("returns %j to %s — a path on this site, or the overview", (state, want) => {
    expect(returnPath(state)).toBe(want);
  });

  it("reads a string field and nothing else", () => {
    expect(stateString({ blocked: "/calls" }, "blocked")).toBe("/calls");
    expect(stateString({ blocked: 1 }, "blocked")).toBeUndefined();
    expect(stateString({}, "blocked")).toBeUndefined();
    expect(stateString(null, "blocked")).toBeUndefined();
  });
});
