import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { ApiProvider } from "../../api/ApiProvider";
import { createApiClients } from "../../api/clients";
import { createApiTransport } from "../../api/transport";
import { ToastProvider } from "../../components/Toast";
import { SessionContext, SessionController } from "../auth/adapter";
import { SessionStore } from "../auth/session";
import { REGISTRAR_URL, server } from "../../test/msw";
import { signedIn, tenantA, TEST_BASE } from "../../test/session";
import { SoftphoneProvider } from "./SoftphoneProvider";
import { PhonePage } from "./PhonePage";
import type { CallState, RegistrationState, Softphone, SoftphoneConnection } from "./client";

class FakeSoftphone implements Softphone {
  delegate: Softphone["delegate"] = { onRegistration() {}, onCall() {} };
  connected: SoftphoneConnection | null = null;
  failConnect = false;
  calls: string[] = [];
  answered = 0;
  rejected = 0;
  hung = 0;
  muted: boolean | null = null;

  async connect(c: SoftphoneConnection) {
    if (this.failConnect) throw new Error("refused");
    this.connected = c;
    this.delegate.onRegistration("registered");
  }
  async disconnect() {
    this.connected = null;
    this.delegate.onRegistration("idle");
  }
  async call(destination: string) {
    this.calls.push(destination);
    this.delegate.onCall("calling", destination);
  }
  async answer() {
    this.answered++;
    this.delegate.onCall("in-call", "1002");
  }
  async reject() {
    this.rejected++;
    this.delegate.onCall("idle");
  }
  async hangup() {
    this.hung++;
    this.delegate.onCall("ended");
  }
  mute(m: boolean) {
    this.muted = m;
  }
  registration(state: RegistrationState, error?: string) {
    this.delegate.onRegistration(state, error);
  }
  incoming(from: string) {
    this.delegate.onCall("incoming", from);
  }
  callState(state: CallState, remote?: string) {
    this.delegate.onCall(state, remote);
  }
}

function renderPhone(client: Softphone, roles = ["TENANT_ADMIN"]) {
  const state = signedIn({ memberships: [{ ...tenantA, roles }] });
  const store = new SessionStore(state);
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  let controller: SessionController | undefined;
  const clients = createApiClients(
    createApiTransport({ baseUrl: TEST_BASE, accessToken: store.accessToken, refresh: () => controller?.refresh() ?? Promise.resolve(false) }),
  );
  controller = new SessionController(clients.auth, store, { onScopeChange: () => queryClient.clear() });
  return render(
    <QueryClientProvider client={queryClient}>
      <ApiProvider clients={clients}>
        <SessionContext.Provider value={controller}>
          <ToastProvider>
            <SoftphoneProvider client={client}>
              <PhonePage />
            </SoftphoneProvider>
          </ToastProvider>
        </SessionContext.Provider>
      </ApiProvider>
    </QueryClientProvider>,
  );
}

const webrtc = { extensionId: "e-1001", number: "1001", displayName: "Reception", deviceType: "DEVICE_TYPE_WEBRTC", authUsername: "e_1001", registration: "REGISTRATION_STATUS_REGISTERED" };
const sip = { extensionId: "e-2001", number: "2001", displayName: "Desk", deviceType: "DEVICE_TYPE_SIP", authUsername: "e_2001", registration: "REGISTRATION_STATUS_REGISTERED" };
const webrtc2 = { ...webrtc, extensionId: "e-1002", number: "1002", displayName: "Desk phone", authUsername: "e_1002" };

function extensions(list: object[]) {
  return http.post("http://localhost:3000/atsapbx.v1.PbxService/ListExtensions", () => HttpResponse.json({ extensions: list }));
}

describe("browser phone", () => {
  it("connects a WEBRTC extension and walks connect, dial, in-call, mute, hangup", async () => {
    const fake = new FakeSoftphone();
    server.use(extensions([webrtc, webrtc2, sip]));
    const user = userEvent.setup();
    renderPhone(fake);
    // Only the WEBRTC extensions are offered.
    const select = await screen.findByLabelText("WEBRTC extension");
    await within(select).findByRole("option", { name: "1001 · Reception" });
    expect(within(select).getAllByRole("option").map((o) => o.textContent)).toEqual(["1001 · Reception", "1002 · Desk phone"]);
    await user.selectOptions(select, "e-1002");
    expect(select).toHaveValue("e-1002");
    await user.selectOptions(select, "e-1001");
    const connect = screen.getByRole("button", { name: "Connect" });
    expect(connect).toBeDisabled();
    await user.type(screen.getByLabelText("SIP password"), "secret");
    await user.click(connect);
    expect(await screen.findByText("Registered as 1001")).toBeInTheDocument();
    expect(fake.connected).toMatchObject({ url: "ws://localhost:8088/ws", username: "e_1001", password: "secret", number: "1001" });

    await user.type(screen.getByLabelText("Number to call"), "1002");
    await user.click(screen.getByRole("button", { name: "Call" }));
    expect(fake.calls).toEqual(["1002"]);
    fake.callState("in-call", "1002");
    expect(await screen.findByText("In call with 1002 · Desk phone")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Mute" }));
    expect(fake.muted).toBe(true);
    await user.click(screen.getByRole("button", { name: "Hang up" }));
    expect(fake.hung).toBe(1);
    expect(await screen.findByText("Call ended")).toBeInTheDocument();
  });

  it("answers and rejects an incoming call", async () => {
    const fake = new FakeSoftphone();
    server.use(extensions([webrtc]));
    const user = userEvent.setup();
    renderPhone(fake);
    await user.type(await screen.findByLabelText("SIP password"), "secret");
    await user.click(screen.getByRole("button", { name: "Connect" }));
    await screen.findByText("Registered as 1001");

    fake.incoming("1002");
    expect(await screen.findByText("Incoming call from 1002")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Answer" }));
    expect(fake.answered).toBe(1);
    expect(await screen.findByText("In call with 1002")).toBeInTheDocument();

    fake.callState("incoming", "1003");
    await user.click(await screen.findByRole("button", { name: "Reject" }));
    expect(fake.rejected).toBe(1);
  });

  it("explains no WEBRTC extension and a missing signalling URL", async () => {
    const fake = new FakeSoftphone();
    server.use(extensions([sip]));
    renderPhone(fake);
    expect(await screen.findByText("No WEBRTC extension yet")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Connect" })).toBeDisabled();
  });

  it("shows the Connect form without a signalling URL", async () => {
    const fake = new FakeSoftphone();
    server.use(
      extensions([webrtc]),
      http.post(REGISTRAR_URL, () => HttpResponse.json({ configured: true, host: "localhost", port: 5060, transport: "udp" })),
    );
    const view = renderPhone(fake);
    expect(await screen.findByText("Browser calling is not configured")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Connect" })).toBeDisabled();
    await userEvent.setup().type(screen.getByLabelText("SIP password"), "secret");
    fireEvent.submit(view.container.querySelector("form")!);
    expect(await screen.findByText("Browser calling is not configured by the installer.")).toBeInTheDocument();
  });

  it("shows a failed connection in console words", async () => {
    const fake = new FakeSoftphone();
    fake.failConnect = true;
    server.use(extensions([webrtc]));
    const user = userEvent.setup();
    renderPhone(fake);
    await user.type(await screen.findByLabelText("SIP password"), "secret");
    await user.click(screen.getByRole("button", { name: "Connect" }));
    expect(await screen.findByText("The browser phone could not connect. Check the extension password and signalling address.")).toBeInTheDocument();
  });

  it("shows connecting, ended and an unknown remote without an engine id", async () => {
    const fake = new FakeSoftphone();
    server.use(extensions([webrtc]));
    const user = userEvent.setup();
    renderPhone(fake);
    await user.type(await screen.findByLabelText("SIP password"), "secret");
    await user.click(screen.getByRole("button", { name: "Connect" }));
    await screen.findByText("Registered as 1001");

    fake.registration("connecting");
    expect(await screen.findByText("Connecting the browser phone…")).toBeInTheDocument();
    fake.registration("registered");
    await screen.findByText("Registered as 1001");

    fake.incoming("e_not_an_extension");
    expect(await screen.findByText("Incoming call from Unknown caller")).toBeInTheDocument();
    expect(document.body.textContent).not.toContain("e_not_an_extension");
    fake.incoming("1001");
    expect(await screen.findByText("Incoming call from 1001 · Reception")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Reject" }));

    fake.callState("ended", "1002");
    expect(await screen.findByText("Call ended")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(screen.getByLabelText("Number to call")).toBeInTheDocument();
  });

  it("does not send a non-number from the browser phone", async () => {
    const fake = new FakeSoftphone();
    server.use(extensions([webrtc]));
    const user = userEvent.setup();
    renderPhone(fake);
    await user.type(await screen.findByLabelText("SIP password"), "secret");
    await user.click(screen.getByRole("button", { name: "Connect" }));
    await screen.findByText("Registered as 1001");
    await user.type(screen.getByLabelText("Number to call"), "not digits");
    await user.click(screen.getByRole("button", { name: "Call" }));
    expect(await screen.findByText("Use digits with an optional leading +, like 1002.")).toBeInTheDocument();
    expect(fake.calls).toEqual([]);
  });

  it("says a role without call permission cannot use the browser phone", async () => {
    const fake = new FakeSoftphone();
    renderPhone(fake, ["AGENT"]);
    expect(await screen.findByText("Your role cannot use the browser phone")).toBeInTheDocument();
  });

  it("validates the password, the URL and the chosen extension", async () => {
    const fake = new FakeSoftphone();
    server.use(extensions([webrtc]));
    const view = renderPhone(fake);
    const select = await screen.findByLabelText("WEBRTC extension");
    await within(select).findByRole("option", { name: "1001 · Reception" });
    const form = view.container.querySelector("form")!;
    fireEvent.submit(form);
    expect(await screen.findByText("Enter the extension's SIP password. It is shown once when the extension is created or rotated.")).toBeInTheDocument();

    server.use(extensions([]));
    view.unmount();
    const empty = renderPhone(fake);
    await screen.findByText("No WEBRTC extension yet");
    fireEvent.submit(empty.container.querySelector("form")!);
    expect(await screen.findByText("Choose a WEBRTC extension.")).toBeInTheDocument();
  });

  it("disconnects the browser phone", async () => {
    const fake = new FakeSoftphone();
    server.use(extensions([webrtc]));
    const user = userEvent.setup();
    renderPhone(fake);
    await user.type(await screen.findByLabelText("SIP password"), "secret");
    await user.click(screen.getByRole("button", { name: "Connect" }));
    await screen.findByText("Registered as 1001");
    await user.click(screen.getByRole("button", { name: "Disconnect" }));
    expect(await screen.findByLabelText("SIP password")).toBeInTheDocument();
  });
});

it("passes the installation's TURN servers to the browser", async () => {
  const fake = new FakeSoftphone();
  server.use(
    extensions([webrtc]),
    http.post(REGISTRAR_URL, () => HttpResponse.json({
      configured: true, host: "localhost", port: 5060, transport: "udp", webrtcUrl: "ws://localhost:8088/ws",
      iceServers: [{ urls: ["turn:turn.example:3478"], username: "browser", credential: "secret" }],
    })),
  );
  const user = userEvent.setup();
  renderPhone(fake);
  await user.type(await screen.findByLabelText("SIP password"), "secret");
  await user.click(screen.getByRole("button", { name: "Connect" }));
  await screen.findByText("Registered as 1001");
  expect(fake.connected?.iceServers).toEqual([
    { urls: "turn:turn.example:3478", username: "browser", credential: "secret" },
  ]);
});

it("maps several TURN URLs and leaves empty credentials unset", async () => {
  const fake = new FakeSoftphone();
  server.use(
    extensions([webrtc]),
    http.post(REGISTRAR_URL, () => HttpResponse.json({
      configured: true, host: "localhost", port: 5060, transport: "udp", webrtcUrl: "ws://localhost:8088/ws",
      iceServers: [{ urls: ["turn:one.example:3478", "turns:two.example:5349"], username: "", credential: "" }],
    })),
  );
  const user = userEvent.setup();
  renderPhone(fake);
  await user.type(await screen.findByLabelText("SIP password"), "secret");
  await user.click(screen.getByRole("button", { name: "Connect" }));
  await screen.findByText("Registered as 1001");
  expect(fake.connected?.iceServers).toEqual([{ urls: ["turn:one.example:3478", "turns:two.example:5349"] }]);
});
