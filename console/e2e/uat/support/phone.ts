import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { createConnection } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

// A real SIP phone for the UAT: baresip on this host, configured from the
// same template as scripts/uat-placement.sh, on the ports a host firewall
// admitting the Docker bridge is expected to allow (5070/5071, RTP
// 18000-18039). Its control socket switches it to auto-answer.

const TEMPLATE = fileURLToPath(new URL("../../../../scripts/uat/baresip-1000-auto/config", import.meta.url));

export interface PhoneOptions {
  sipPort: number;
  ctrlPort: number;
  rtpPorts: string;
  // account is one baresip accounts line.
  account: string;
}

export class Phone {
  private readonly dir: string;
  private readonly proc: ChildProcess;
  private output = "";

  constructor(private readonly options: PhoneOptions) {
    this.dir = mkdtempSync(join(tmpdir(), "console-uat-phone-"));
    mkdirSync(this.dir, { recursive: true });
    const config = readFileSync(TEMPLATE, "utf8")
      .replace(/^sip_listen.*$/m, `sip_listen\t\t0.0.0.0:${options.sipPort}`)
      .replace(/^ctrl_tcp_listen.*$/m, `ctrl_tcp_listen\t\t127.0.0.1:${options.ctrlPort}`)
      .replace(/^rtp_ports.*$/m, `rtp_ports\t\t${options.rtpPorts}`);
    writeFileSync(join(this.dir, "config"), config);
    writeFileSync(join(this.dir, "accounts"), `${options.account}\n`);
    this.proc = spawn("baresip", ["-f", this.dir], { stdio: ["ignore", "pipe", "pipe"] });
    this.proc.stdout?.on("data", (b: Buffer) => (this.output += b.toString()));
    this.proc.stderr?.on("data", (b: Buffer) => (this.output += b.toString()));
  }

  get log(): string {
    return this.output;
  }

  // command sends one control command (netstring JSON) and returns the reply.
  command(command: string, params = ""): Promise<string> {
    return new Promise((done, fail) => {
      const payload = JSON.stringify({ command, params, token: "console-uat" });
      const socket = createConnection({ host: "127.0.0.1", port: this.options.ctrlPort });
      let reply = "";
      socket.setTimeout(3000, () => socket.end());
      socket.on("data", (b) => {
        reply += b.toString();
        if (reply.endsWith(",")) socket.end();
      });
      socket.on("end", () => done(reply));
      socket.on("error", fail);
      socket.write(`${payload.length}:${payload},`);
    });
  }

  async autoAnswer(): Promise<void> {
    for (let i = 0; i < 20; i++) {
      try {
        if ((await this.command("answermode", "auto")).includes("changed to: auto")) return;
      } catch {
        // the control socket is not up yet
      }
      await new Promise((r) => setTimeout(r, 250));
    }
    throw new Error(`baresip on ${this.options.ctrlPort} would not switch to auto-answer:\n${this.output}`);
  }

  stop(): void {
    this.proc.kill("SIGTERM");
    rmSync(this.dir, { recursive: true, force: true });
  }
}

export function portsFree(...ports: number[]): boolean {
  const listening = execFileSync("ss", ["-lun"], { encoding: "utf8" });
  return ports.every((p) => !listening.includes(`:${p} `));
}

export function baresipInstalled(): boolean {
  try {
    execFileSync("which", ["baresip"], { stdio: "ignore" });
    return true;
  } catch {
    return false;
  }
}

// engineReachesHostAt is the address the media engine reaches this host
// by: the gateway of its Docker network (scripts/uat-placement.sh).
export function engineReachesHostAt(): string {
  return execFileSync("docker", ["inspect", "deploy-asterisk-1", "--format", "{{range .NetworkSettings.Networks}}{{.Gateway}}{{end}}"], { encoding: "utf8" }).trim();
}
