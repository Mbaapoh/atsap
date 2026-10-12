import { Code, ConnectError } from "@connectrpc/connect";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { server } from "../test/msw";
import { createApiClients } from "./clients";
import { ApiError, callApi, toApiError } from "./errors";
import { PbxService } from "./gen/atsapbx/v1/pbx_pb";
import { isPublicProcedure, PUBLIC_PROCEDURES, PUBLIC_SERVICES } from "./publicApi";
import { createApiTransport } from "./transport";

const base = "http://localhost:3000";

describe("public API only", () => {
  it("lists every procedure of every public service, from the descriptors", () => {
    expect(PUBLIC_SERVICES.map((s) => s.typeName).sort()).toEqual([
      "atsapbx.v1.AuthService",
      "atsapbx.v1.IdentityService",
      "atsapbx.v1.LicensingService",
      "atsapbx.v1.PbxService",
      "atsapbx.v1.TelephonyService",
    ]);
    expect(PUBLIC_PROCEDURES.has("/atsapbx.v1.LicensingService/GetLicenseStatus")).toBe(true);
    expect(PUBLIC_PROCEDURES.size).toBe(PUBLIC_SERVICES.reduce((n, s) => n + s.methods.length, 0));
  });

  it.each([
    ["/healthz", false],
    ["/readyz", false],
    ["/atsapbx.v1.LicensingService/NoSuchMethod", false],
    ["/atsapbx.v1.InternalService/Anything", false],
    ["https://elsewhere.example/atsapbx.v1.LicensingService/GetLicenseStatus", false],
    ["/atsapbx.v1.LicensingService/GetLicenseStatus", true],
    [`${base}/atsapbx.v1.PbxService/ListExtensions`, true],
  ])("treats %s as public: %s", (url, expected) => {
    expect(isPublicProcedure(url, base)).toBe(expected);
  });

  it("refuses, before sending, a call the server does not publish", async () => {
    let sent = false;
    server.use(
      http.post(`${base}/atsapbx.v1.PbxService/Secret`, () => {
        sent = true;
        return HttpResponse.json({});
      }),
    );
    // A hand-made descriptor naming a procedure the protos do not define:
    // the shape a stray, hand-built call would take.
    const rogue = { ...PbxService, methods: [{ ...PbxService.methods[0]!, name: "Secret", localName: "secret" }] };
    const transport = createApiTransport({ baseUrl: base });
    const method = rogue.methods[0]!;
    const err = await transport
      .unary(method as never, undefined, undefined, undefined, {} as never)
      .catch((e: unknown) => e);

    expect(sent).toBe(false);
    expect(err).toBeInstanceOf(ConnectError);
    expect((err as ConnectError).code).toBe(Code.PermissionDenied);
  });
});

describe("errors reaching components", () => {
  it("carry fixed console copy, never the server's message", async () => {
    server.use(
      http.post(`${base}/atsapbx.v1.PbxService/ListExtensions`, () =>
        HttpResponse.json({ code: "internal", message: "pq: relation pjsip_endpoints ext-1001 missing" }, { status: 500 }),
      ),
    );
    const clients = createApiClients(createApiTransport({ baseUrl: base }));
    const err = await callApi(() => clients.pbx.listExtensions({})).catch((e: unknown) => e);

    expect(err).toBeInstanceOf(ApiError);
    expect((err as ApiError).kind).toBe("unknown");
    expect((err as ApiError).message).not.toMatch(/pq|pjsip|ext-1001|relation/);
    expect(Object.keys(err as object)).not.toContain("cause");
  });

  it.each([
    [Code.Unauthenticated, "unauthenticated"],
    [Code.PermissionDenied, "forbidden"],
    [Code.NotFound, "not-found"],
    [Code.AlreadyExists, "conflict"],
    [Code.FailedPrecondition, "conflict"],
    [Code.InvalidArgument, "invalid"],
    [Code.ResourceExhausted, "throttled"],
    [Code.Unavailable, "unavailable"],
    [Code.Internal, "unknown"],
  ])("map code %s to %s", (code, kind) => {
    expect(toApiError(new ConnectError("secret detail", code)).kind).toBe(kind);
    expect(toApiError(new ConnectError("secret detail", code)).message).not.toContain("secret detail");
  });

  it("map a non-Connect throw to unknown", () => {
    expect(toApiError(new TypeError("Failed to fetch")).kind).toBe("unknown");
  });
});
