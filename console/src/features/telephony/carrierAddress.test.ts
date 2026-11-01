import { describe, expect, it } from "vitest";
import { carrierHost, checkInbound, inboundLines } from "./carrierAddress";

// pbx-carrier-addresses (D-76): the console mirrors the server's rules so
// a mistake is shown at its field. The server still decides.
describe("carrier addresses", () => {
  it("refuses hosts that cannot be a carrier, and only those", () => {
    for (const h of ["localhost", "LOCALHOST", "localhost.", "pbx.localhost", "127.0.0.1", "127.8.9.10", "0.0.0.0", "0.1.2.3", "169.254.1.1", "224.0.0.1", "255.255.255.255", "::1", "::", "fe80::1", "ff02::1", "::ffff:127.0.0.1"])
      expect({ host: h, check: carrierHost(h) }).toEqual({ host: h, check: "not-carrier" });
    for (const h of ["sip.carrier.example", "mylocalhost", "localhost.example", "10.20.0.5", "192.168.1.1", "172.18.0.5", "203.0.113.10", "2001:db8::10", "fd00::1"])
      expect({ host: h, check: carrierHost(h) }).toEqual({ host: h, check: "ok" });
  });

  it("reads one entry per line, trimmed, blank lines dropped", () => {
    expect(inboundLines(" 203.0.113.9 \n\n198.51.100.0/24\r\n  \n")).toEqual(["203.0.113.9", "198.51.100.0/24"]);
    expect(inboundLines("")).toEqual([]);
  });

  it("accepts addresses and ranges a carrier publishes", () => {
    expect(checkInbound(["203.0.113.9", "198.51.100.0/24", "10.0.0.0/16", "192.168.10.0/24", "2001:db8::/32", "2001:db8::10"])).toBeNull();
    expect(checkInbound([])).toBeNull();
  });

  it("names the line and the problem", () => {
    expect(checkInbound(["203.0.113.9", "sip.carrier.example"])).toMatch(/^Line 2 \(sip.carrier.example\) is not an IP address or range/);
    expect(checkInbound(["203.0.113.0/33"])).toMatch(/not an IP address or range/);
    expect(checkInbound(["203.0.113.0/2a"])).toMatch(/not an IP address or range/);
    expect(checkInbound(["203.0.113.0/24/1"])).toMatch(/not an IP address or range/);
    expect(checkInbound(["203.0.113.256"])).toMatch(/not an IP address or range/);
    expect(checkInbound(["203.0.113.01"])).toMatch(/not an IP address or range/);
    expect(checkInbound(["2001:db8::/129"])).toMatch(/not an IP address or range/);
    expect(checkInbound(["fe80::1%eth0"])).toMatch(/not an IP address or range/);
    expect(checkInbound(["10.0.0.0/15"])).toMatch(/^Line 1 \(10.0.0.0\/15\) is too broad/);
    expect(checkInbound(["2001:db8::/31"])).toMatch(/too broad/);
    expect(checkInbound(["127.0.0.1"])).toMatch(/cannot be a carrier/);
    expect(checkInbound(["169.254.0.0/16"])).toMatch(/cannot be a carrier/);
    expect(checkInbound(["::1"])).toMatch(/cannot be a carrier/);
    expect(checkInbound(["ff02::/32"])).toMatch(/cannot be a carrier/);
  });

  it("takes at most twenty", () => {
    const twenty = Array.from({ length: 20 }, (_, i) => `203.0.113.${i}`);
    expect(checkInbound(twenty)).toBeNull();
    expect(checkInbound([...twenty, "203.0.113.99"])).toBe("Enter at most 20 addresses or ranges.");
  });
});
