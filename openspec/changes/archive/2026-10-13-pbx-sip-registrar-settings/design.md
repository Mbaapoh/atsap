## Context

pbx-core already carries the engine's SIP settings in `config.Config`:
- `SIPRealm`, the realm credentials are digested over;
- `SIPTransport` and `SIPWebRTCTransport`, PJSIP transport names.

None of them is the address a device dials, which depends on how the installation is exposed: a public IP or DNS name, NAT, a port mapping. Only the installer knows it. See proposal.md.

## Goals / Non-Goals

**Goals:**
- One read that tells a console or a partner's software where phones register, from the installation's own configuration.
- An honest "not configured" answer.

**Non-Goals:**
- Discovering the address automatically (STUN, interface probing). It would be wrong behind NAT, which is the common case.
- Per-tenant registrars. One installation has one registrar.
- WebRTC (WSS) connection details, which come with the WebRTC transport work.

## Decisions

### 1. A dedicated read RPC, not a field on every Extension

`GetSipRegistrar` is installation-wide and the same for every extension, so repeating it in each `Extension` would bloat every list. The console reads it once for the credential panel. It is authorized with the existing extension-read action, because whoever can see extensions may see where they register.

### 2. Configuration names follow `SIP_REALM`

The settings are `SIP_PUBLIC_HOST`, `SIP_PUBLIC_PORT` and `SIP_PUBLIC_TRANSPORT`, unprefixed like the existing SIP settings.
- The host has no default: empty means not configured, and the API says so.
- The port defaults to 5060, the SIP default.
- The transport defaults to `udp` and must be `udp`, `tcp` or `tls`, checked at startup.
- The port must be between 1 and 65535, checked at startup.
- A host must be a hostname or IP address, with no scheme, port or user part, using the same rule as trunk hosts. A bad value fails startup loudly, not at a phone.

### 3. The dev rig sets `localhost`

Compose sets `SIP_PUBLIC_HOST=localhost` for the app: the rig's Asterisk publishes 5060 on the host, so a softphone on the same machine registers to `localhost:5060`. `.env.example` explains that real phones need the machine's LAN address or DNS name.

## Risks / Trade-offs

- **[Risk] A wrong configured host.** → The API reports exactly what the installer set; a mistake shows up the first time a phone fails, as with any SIP setup. The console labels the value as the installation's registrar so the source is clear.
- **[Trade-off] Not auto-discovered.** → Deliberate: discovery behind NAT returns private addresses that no remote phone can reach.

## Migration Plan

Additive:
- A proto field and an RPC are added (`buf breaking` stays clean).
- The new settings have safe defaults (unset means not configured).
- Rollback: remove the RPC; no data is involved.
