# Coverage — pbx-sip-registrar-settings

**Tiers:**
- **U**: unit (`internal/config`).
- **I**: integration (`pbx/application`, real Postgres).
- **W**: wire (`pbx/rpc` handler).
- **E**: e2e (`internal/pbx/e2e/console_apis_test.go`, running app).

## pbx-core/extension-management

### The public API reports where devices register

| Scenario | Proven by |
|---|---|
| An administrator reads the registrar address | **E** `TestConsoleAPIs_SipRegistrar` (the rig's localhost:5060/udp). **I** `TestSipRegistrar_ReturnsTheConfiguredAddress` (authorized as `extension.read` on the tenant). **W** `TestGetSipRegistrar_MapsEveryField` |
| No public host is configured | **I** `TestSipRegistrar_NotConfiguredWithoutAHost` (no stray port or transport). **W** `TestGetSipRegistrar_NotConfigured` |
| Reading the address requires the extension-read permission | **E** a supervisor is refused with 403. **I** `TestSipRegistrar_RefusedWithoutPermission` (fault-injected: removing the authorization fails it). **W** `TestGetSipRegistrar_PermissionRefusalMaps` |
| Another tenant cannot be named | **W** `TestGetSipRegistrar_RefusesAnotherTenantAndUnauthenticated` |

**Configuration:** `TestLoad_SIPPublicRegistrar` and `TestLoad_SIPPublicRegistrarRejectsBadValues` (**U**). Seven invalid values each fail startup with a message naming the setting.

## Gaps

| # | Gap | Severity | Note |
|---|---|---|---|
| G1 | The address is not auto-discovered | Accepted | Deliberate (design): discovery behind NAT yields private addresses; the installer configures it |
| G2 | No WebRTC (WSS) connection details | Low | They arrive with the WebRTC transport work |
