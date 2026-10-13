## Why

The console can create an extension and show its SIP username and secret once. It cannot say *where* a phone should register, because no public API exposes the installation's SIP registrar address. HLD-18 Flow A3 ("the admin provisions the phone") therefore cannot be completed in the console, and §7 requires A1–A5 without leaving it. The owner decided on 2026-10-13 to add the address to the API rather than send people to an installer; this is a dependency of `console-admin-screens`.

## What Changes

- **New read RPC:** `PbxService.GetSipRegistrar(tenant_id)` returns the address devices register to:
  - host;
  - port;
  - transport (`udp`, `tcp` or `tls`);
  - `configured`, false when the installation has not set a public host, in which case the response says so rather than guessing.

  It is authorized like reading extensions.
- **Configuration:**
  - `SIP_PUBLIC_HOST` (no default: unset means not configured);
  - `SIP_PUBLIC_PORT` (default 5060);
  - `SIP_PUBLIC_TRANSPORT` (default `udp`).

  These sit beside the existing `SIP_REALM`, are validated at startup, and are set for the dev rig in compose.
- **Docs:** API.md, LLD-03, `.env.example`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `pbx-core/extension-management`: adds the requirement that the public API reports the registrar address devices register to, or that none is configured.

## Impact

- **Code:**
  - `api/proto/atsapbx/v1/pbx.proto` (additive; `buf breaking` clean);
  - `api/internal/config` (three settings and their validation);
  - `api/internal/pbx` (port method, application read with authorization, RPC handler);
  - `api/cmd/atsap-api` wiring;
  - regenerated Go and console clients.
- **Deploy:** `deploy/docker-compose.yml` sets `SIP_PUBLIC_HOST` for the rig, and `.env.example` documents it.
- **Security:** the address is the one already reachable by every phone of the installation. It is no secret and reveals nothing tenant-specific. Authorization still applies, so an unauthenticated caller gets nothing.
