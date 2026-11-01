// Carrier addresses, checked in the console as the server checks them
// (pbx/domain/trunk_addresses.go, D-76), so a mistake is shown at its
// field before anything is sent. The server still decides.

const MAX_INBOUND = 20;

// parseIPv4 returns the four octets, or null.
function parseIPv4(s: string): number[] | null {
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(s);
  if (!m) return null;
  const octets = m.slice(1).map(Number);
  return octets.every((o, i) => o <= 255 && String(o) === m[i + 1]) ? octets : null;
}

// canonicalIPv6 is the address in the browser's own canonical form, or
// null: the URL parser is the platform's IPv6 parser.
function canonicalIPv6(s: string): string | null {
  if (!s.includes(":") || /[[\]%/]/.test(s)) return null;
  try {
    return new URL(`http://[${s}]/`).hostname.slice(1, -1).toLowerCase();
  } catch {
    return null;
  }
}

// ipv4Carrier and ipv6Carrier mirror domain.CarrierAddress: never
// loopback, link-local, unspecified, "this network" or multicast.
function ipv4Carrier([a = 0, b = 0]: number[]): boolean {
  return !(a === 0 || a === 127 || (a === 169 && b === 254) || a >= 224);
}

function ipv6Carrier(canonical: string): boolean {
  if (canonical === "::" || canonical === "::1" || canonical.startsWith("::ffff:")) return false;
  return !/^fe[89ab]/.test(canonical) && !canonical.startsWith("ff");
}

export type HostCheck = "ok" | "not-carrier";

// carrierHost says whether a syntactically valid host can be a carrier:
// localhost and the loopback, link-local, unspecified and multicast
// literals cannot.
export function carrierHost(host: string): HostCheck {
  const name = host.toLowerCase().replace(/\.$/, "");
  if (name === "localhost" || name.endsWith(".localhost")) return "not-carrier";
  const v4 = parseIPv4(host);
  if (v4) return ipv4Carrier(v4) ? "ok" : "not-carrier";
  const v6 = canonicalIPv6(host);
  if (v6) return ipv6Carrier(v6) ? "ok" : "not-carrier";
  return "ok";
}

// inboundLines is the textarea's content as entries: trimmed, blank lines
// dropped.
export function inboundLines(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter((l) => l !== "");
}

// checkInbound returns console copy for the first problem, or null.
export function checkInbound(entries: readonly string[]): string | null {
  if (entries.length > MAX_INBOUND) return `Enter at most ${MAX_INBOUND} addresses or ranges.`;
  for (const [i, entry] of entries.entries()) {
    const line = `Line ${i + 1} (${entry})`;
    const [addr = "", bits, ...rest] = entry.split("/");
    if (rest.length || (bits !== undefined && !/^\d{1,3}$/.test(bits))) return `${line} is not an IP address or range, like 203.0.113.0/28.`;
    const v4 = parseIPv4(addr);
    const v6 = v4 ? null : canonicalIPv6(addr);
    if (!v4 && !v6) return `${line} is not an IP address or range, like 203.0.113.0/28.`;
    const width = v4 ? 32 : 128;
    const prefix = bits === undefined ? width : Number(bits);
    if (prefix > width) return `${line} is not an IP address or range, like 203.0.113.0/28.`;
    if ((v4 && prefix < 16) || (v6 && prefix < 32)) return `${line} is too broad: use /16 or narrower for IPv4, /32 or narrower for IPv6.`;
    if (v4 ? !ipv4Carrier(v4) : !ipv6Carrier(v6 ?? "")) return `${line} cannot be a carrier: it is a loopback, link-local or multicast address.`;
  }
  return null;
}
