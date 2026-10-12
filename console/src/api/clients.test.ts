import { Code, ConnectError } from "@connectrpc/connect";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { server } from "../test/msw";
import { createApiClients } from "./clients";
import { LicenseState } from "./gen/atsapbx/v1/licensing_pb";
import { createApiTransport } from "./transport";

// Proof that the generated descriptors drive real Connect calls: a typed
// client posts to the public procedure path, decodes the response into
// generated types, and maps a Connect error code.
const base = "http://localhost:3000";

describe("generated ConnectRPC clients", () => {
  it("call the public procedure with the in-memory token and decode typed responses", async () => {
    let seen: { path: string; auth: string | null; body: unknown } | undefined;
    server.use(
      http.post(`${base}/atsapbx.v1.LicensingService/GetLicenseStatus`, async ({ request }) => {
        seen = { path: new URL(request.url).pathname, auth: request.headers.get("authorization"), body: await request.json() };
        return HttpResponse.json({ status: { state: "LICENSE_STATE_VALID", edition: "development", channels: 16, channelsInUse: 2 } });
      }),
    );
    const clients = createApiClients(createApiTransport({ baseUrl: base, accessToken: () => "token-in-memory" }));

    const res = await clients.licensing.getLicenseStatus({});

    expect(seen?.path).toBe("/atsapbx.v1.LicensingService/GetLicenseStatus");
    expect(seen?.auth).toBe("Bearer token-in-memory");
    expect(res.status?.state).toBe(LicenseState.VALID);
    expect(res.status?.channels).toBe(16);
    expect(res.status?.channelsInUse).toBe(2);
  });

  it("send no Authorization header when no token is held", async () => {
    let auth: string | null = "unset";
    server.use(
      http.post(`${base}/atsapbx.v1.AuthService/DiscoverSignIn`, ({ request }) => {
        auth = request.headers.get("authorization");
        return HttpResponse.json({ method: "SIGN_IN_METHOD_PASSWORD" });
      }),
    );
    const clients = createApiClients(createApiTransport({ baseUrl: base }));
    await clients.auth.discoverSignIn({ email: "ana@example.com" });
    expect(auth).toBeNull();
  });

  it("surface a server refusal as a typed Connect error code", async () => {
    server.use(
      http.post(`${base}/atsapbx.v1.LicensingService/ApplyLicenseKey`, () =>
        HttpResponse.json({ code: "permission_denied", message: "not permitted" }, { status: 403 }),
      ),
    );
    const clients = createApiClients(createApiTransport({ baseUrl: base }));
    const err = await clients.licensing.applyLicenseKey({ token: "x" }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ConnectError);
    expect((err as ConnectError).code).toBe(Code.PermissionDenied);
  });

  it("expose every Phase A service the console consumes", () => {
    const clients = createApiClients(createApiTransport({ baseUrl: base }));
    expect(Object.keys(clients).sort()).toEqual(["auth", "identity", "licensing", "pbx", "telephony"]);
    expect(typeof clients.pbx.listExtensions).toBe("function");
    expect(typeof clients.telephony.listCalls).toBe("function");
    expect(typeof clients.identity.listPrincipals).toBe("function");
  });
});
