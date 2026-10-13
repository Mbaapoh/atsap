import { http, HttpResponse } from "msw";
import { setupServer } from "msw/node";

// LICENCE_STATUS_URL is the call the shell makes on every page (the
// top-bar licence pill).
export const LICENCE_STATUS_URL = "http://localhost:3000/atsapbx.v1.LicensingService/GetLicenseStatus";

// validLicence is a licence in force; tests about other conditions
// override the handler with server.use().
export const validLicence = {
  state: "LICENSE_STATE_VALID",
  edition: "standard",
  channels: 16,
  channelsInUse: 2,
  condition: "LICENSE_CONDITION_VALID",
  tier: "LICENSE_TIER_COMMERCIAL",
  capacity: "CAPACITY_LEVEL_NORMAL",
};

// Component tests intercept HTTP in Node (MSW's server-side interception,
// not a browser service worker). Unhandled requests fail the test, so no
// test can quietly reach a real network. The one default handler is the
// licence status every signed-in page reads.
const api = (procedure: string) => `http://localhost:3000/atsapbx.v1.${procedure}`;

// The overview's reads default to an empty tenant: no other users, no
// extensions, trunks, routes or calls.
export const OVERVIEW_URLS = {
  principals: api("IdentityService/ListPrincipals"),
  extensions: api("PbxService/ListExtensions"),
  trunks: api("PbxService/ListTrunks"),
  routes: api("PbxService/ListRoutes"),
  calls: api("TelephonyService/ListCalls"),
};

export const REGISTRAR_URL = api("PbxService/GetSipRegistrar");
export const INBOUND_URL = api("PbxService/ListInboundNumbers");

export const server = setupServer(
  http.post(LICENCE_STATUS_URL, () => HttpResponse.json({ status: validLicence })),
  http.post(OVERVIEW_URLS.principals, () => HttpResponse.json({ principals: [{}] })),
  http.post(OVERVIEW_URLS.extensions, () => HttpResponse.json({})),
  http.post(OVERVIEW_URLS.trunks, () => HttpResponse.json({})),
  http.post(OVERVIEW_URLS.routes, () => HttpResponse.json({})),
  http.post(OVERVIEW_URLS.calls, () => HttpResponse.json({})),
  http.post(INBOUND_URL, () => HttpResponse.json({})),
  http.post(REGISTRAR_URL, () => HttpResponse.json({ configured: true, host: "localhost", port: 5060, transport: "udp" })),
);
