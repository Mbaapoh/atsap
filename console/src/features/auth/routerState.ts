// Router state is whatever was passed to navigate(): unknown until read.
// These read it without trusting its shape.

// stateString reads one string field of router state, if there is one.
export function stateString(state: unknown, key: string): string | undefined {
  if (typeof state !== "object" || state === null) return undefined;
  const value: unknown = Reflect.get(state, key);
  return typeof value === "string" ? value : undefined;
}

// returnPath is where to go after signing in or choosing a tenant: the
// page the guard sent the person away from, kept in router memory only,
// and only ever a path on this site — never "//elsewhere".
export function returnPath(state: unknown): string {
  const from = stateString(state, "from");
  return from !== undefined && from.startsWith("/") && !from.startsWith("//") ? from : "/";
}
