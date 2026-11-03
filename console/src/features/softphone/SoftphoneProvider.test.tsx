import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { SoftphoneProvider, useSoftphone } from "./SoftphoneProvider";

// The dynamic import is mocked so the provider's lazy SIP.js loading and
// its failure paths are testable without a browser or an engine.
vi.mock("./client", () => {
  class MockClient {
    delegate: { onRegistration(state: string, error?: string): void; onCall(state: string, remote?: string, error?: string): void } = {
      onRegistration() {},
      onCall() {},
    };
    async connect(c: { password: string }) {
      if (c.password === "fail") throw new Error("sip failed");
      this.delegate.onRegistration("registered");
    }
    async disconnect() {
      this.delegate.onRegistration("idle");
    }
    async call() {}
    async answer() {}
    async reject() {}
    async hangup() {}
    mute() {}
  }
  return { SipSoftphone: MockClient };
});

function Probe() {
  const phone = useSoftphone();
  return (
    <div>
      <p>{phone.registration}</p>
      <p>{phone.error ?? "no-error"}</p>
      <button type="button" onClick={() => void phone.connect({ url: "ws://x", username: "u", password: "p", domain: "d" })}>
        connect
      </button>
      <button type="button" onClick={() => void phone.connect({ url: "ws://x", username: "u", password: "fail", domain: "d" })}>
        fail
      </button>
      <button type="button" onClick={() => void phone.disconnect()}>
        disconnect
      </button>
    </div>
  );
}

describe("softphone provider", () => {
  it("loads the SIP client lazily, connects and disconnects", async () => {
    const user = userEvent.setup();
    render(
      <SoftphoneProvider>
        <Probe />
      </SoftphoneProvider>,
    );
    await user.click(screen.getByRole("button", { name: "connect" }));
    expect(await screen.findByText("registered")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "disconnect" }));
    expect(await screen.findByText("idle")).toBeInTheDocument();
  });

  it("reports a client that cannot connect", async () => {
    const user = userEvent.setup();
    render(
      <SoftphoneProvider>
        <Probe />
      </SoftphoneProvider>,
    );
    await user.click(screen.getByRole("button", { name: "fail" }));
    expect(await screen.findByText("error")).toBeInTheDocument();
    expect(screen.getByText("The browser phone could not connect. Check the extension password and signalling address.")).toBeInTheDocument();
  });
});
