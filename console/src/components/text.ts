// codePoints counts a string's Unicode code points, which is how the
// server measures every length it limits (Go's utf8.RuneCountInString):
// the console must refuse exactly what the server would. Not graphemes —
// "é" typed as e + a combining accent is two, here and on the server.
export function codePoints(s: string): number {
  return Array.from(s).length;
}
