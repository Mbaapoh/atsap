import { describe, expect, it } from "vitest";
import { passwordProblem } from "../auth/adapter";
import { TEMPORARY_ALPHABET, TEMPORARY_LENGTH, temporaryPassword } from "./temporaryPassword";

describe("temporaryPassword", () => {
  it("is 16 characters from the unambiguous alphabet, within the server's bounds", () => {
    for (let i = 0; i < 200; i++) {
      const p = temporaryPassword();
      expect(p).toHaveLength(TEMPORARY_LENGTH);
      expect([...p].every((c) => TEMPORARY_ALPHABET.includes(c))).toBe(true);
      expect(passwordProblem(p)).toBeNull();
      expect(p).not.toMatch(/[0O1lI]/);
    }
  });

  it("does not repeat", () => {
    const seen = new Set(Array.from({ length: 500 }, () => temporaryPassword()));
    expect(seen.size).toBe(500);
  });

  it("rejects the biased high bytes instead of folding them", () => {
    // 224 is the largest multiple of 56 that fits a byte (256 % 56 = 32),
    // so bytes 224-255 must be skipped, never folded onto the alphabet.
    let call = 0;
    const p = temporaryPassword(() => {
      call++;
      return call === 1 ? new Uint8Array(TEMPORARY_LENGTH).fill(250) : new Uint8Array(TEMPORARY_LENGTH).fill(0);
    });
    expect(p).toBe(TEMPORARY_ALPHABET[0]!.repeat(TEMPORARY_LENGTH));
  });
});
