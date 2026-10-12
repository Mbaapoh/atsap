import { describe, expect, it } from "vitest";
import tokens from "./tokens.css?raw";

// A gate on the real token values: every text/background pairing the
// components use meets WCAG 2.2 AA (4.5:1 for text, 3:1 for the focus
// ring and other non-text indicators). Change a token and this fails
// before anyone ships an unreadable badge.
function token(name: string): string {
  const m = tokens.match(new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{6})`));
  if (!m) throw new Error(`token --${name} not found`);
  return m[1]!;
}

function luminance(hex: string): number {
  const c = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
    .map((v) => (v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
  return 0.2126 * c[0]! + 0.7152 * c[1]! + 0.0722 * c[2]!;
}

function contrast(a: string, b: string): number {
  const [x, y] = [luminance(token(a)), luminance(token(b))];
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

describe("token contrast (WCAG 2.2 AA)", () => {
  it.each([
    ["color-text", "color-surface"], ["color-text", "color-canvas"], ["color-text", "color-surface-muted"],
    ["color-text-muted", "color-surface"], ["color-text-muted", "color-canvas"], ["color-text-muted", "color-surface-muted"],
    ["color-on-primary", "color-action-primary"], ["color-on-primary", "color-action-primary-hover"],
    ["color-on-primary", "color-danger"], ["color-nav-text", "color-nav"],
    ["color-success", "color-success-surface"], ["color-warning", "color-warning-surface"],
    ["color-danger", "color-danger-surface"], ["color-info", "color-info-surface"], ["color-info", "color-canvas"],
    ["color-success", "color-surface"], ["color-warning", "color-surface"], ["color-danger", "color-surface"], ["color-info", "color-surface"],
    ["color-text", "color-success-surface"], ["color-text", "color-warning-surface"],
    ["color-text", "color-danger-surface"], ["color-text", "color-info-surface"],
  ])("text %s on %s is at least 4.5:1", (fg, bg) => {
    expect(contrast(fg, bg)).toBeGreaterThanOrEqual(4.5);
  });

  it.each([
    ["color-focus", "color-surface"], ["color-focus", "color-canvas"], ["color-focus-on-dark", "color-nav"],
  ])("focus ring %s on %s is at least 3:1", (fg, bg) => {
    expect(contrast(fg, bg)).toBeGreaterThanOrEqual(3);
  });
});
