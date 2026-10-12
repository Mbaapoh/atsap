import { createClient, type Client, type Transport } from "@connectrpc/connect";
import { AuthService } from "./gen/atsapbx/v1/auth_pb";
import { IdentityService } from "./gen/atsapbx/v1/identity_pb";
import { LicensingService } from "./gen/atsapbx/v1/licensing_pb";
import { PbxService } from "./gen/atsapbx/v1/pbx_pb";
import { TelephonyService } from "./gen/atsapbx/v1/telephony_pb";

// ApiClients are the console's only network boundary (HLD-19 §3): typed
// clients generated from the same protos the server is built from, one per
// public service (publicApi.ts). Feature adapters wrap them; visual
// components never call them directly.
export interface ApiClients {
  auth: Client<typeof AuthService>;
  identity: Client<typeof IdentityService>;
  licensing: Client<typeof LicensingService>;
  pbx: Client<typeof PbxService>;
  telephony: Client<typeof TelephonyService>;
}

export function createApiClients(transport: Transport): ApiClients {
  return {
    auth: createClient(AuthService, transport),
    identity: createClient(IdentityService, transport),
    licensing: createClient(LicensingService, transport),
    pbx: createClient(PbxService, transport),
    telephony: createClient(TelephonyService, transport),
  };
}
