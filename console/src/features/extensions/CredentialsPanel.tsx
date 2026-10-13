import { Alert } from "../../components/Alert";
import { OneTimeSecret, type SecretField } from "../../components/OneTimeSecret";
import { useRegistrar, type Credentials } from "./adapter";

// CredentialsPanel shows what a phone needs to register, once: the
// registrar address, the SIP username (labelled — it is also the engine's
// endpoint id, the one sanctioned place it appears, console-admin-screens
// D3) and the secret. Used at creation and after a secret rotation.
export function CredentialsPanel({ credentials, title, onDone }: { credentials: Credentials | null; title: string; onDone: () => void }) {
  const registrar = useRegistrar();
  const fields: SecretField[] = [];
  if (credentials) {
    if (registrar.data) {
      fields.push({ label: "SIP server", value: registrar.data.host }, { label: "Port", value: String(registrar.data.port) }, { label: "Transport", value: registrar.data.transport.toUpperCase() });
    }
    fields.push({ label: "SIP username", value: credentials.extension.sipUsername, sipUsername: true }, { label: "SIP password", value: credentials.secret });
  }
  return (
    <OneTimeSecret
      open={credentials !== null}
      title={title}
      intro={
        <>
          <p>
            Enter these in the phone for extension {credentials?.extension.number}. The password is shown only now; if it is lost, rotate it and
            reconfigure the phone.
          </p>
          {registrar.data === null ? (
            <Alert tone="warning" title="The SIP server address is not configured">
              <p>Your installer has not set this installation&apos;s public SIP address. Ask them for it before configuring the phone.</p>
            </Alert>
          ) : null}
          {registrar.isError ? (
            <Alert tone="warning" title="The SIP server address could not be read">
              <p>The phone details below are still correct. Ask your installer for the SIP server address.</p>
            </Alert>
          ) : null}
        </>
      }
      fields={fields}
      onDone={onDone}
    />
  );
}
