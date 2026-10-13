import type { Page, Request, Response } from "@playwright/test";

// Exposure checks for console-foundation task 3.3: what the console sends
// and receives on the wire, and what it renders, must never carry a
// credential out of place or the media engine's internal vocabulary
// (console brief §6 rules 1–3, 7 and 10; PRD AC-10.6).

// ENGINE_PATTERNS are the media engine's identifiers and vocabulary.
export const ENGINE_PATTERNS: { name: string; pattern: RegExp }[] = [
  { name: "engine endpoint id", pattern: /\b[et]_[0-9a-f]{32}\b/ },
  { name: "channel technology dial string", pattern: /\b(?:PJSIP|SIP|IAX2|Local|DAHDI)\/[\w@.+-]+/ },
  { name: "Asterisk channel unique id", pattern: /\b1[6-9]\d{8}\.\d+\b/ },
  { name: "bridge id", pattern: /\bbridge[-_][0-9a-f-]{8,}/i },
  { name: "realtime table", pattern: /\bps_(?:endpoints|aors|auths|contacts|endpoint_id_ips|registrations|globals)\b/ },
  { name: "configuration file", pattern: /\b[\w-]+\.conf\b/ },
  { name: "dialplan context", pattern: /\bstasis-(?:in|out)\b/ },
];

// The SIP username of an extension IS its engine endpoint id (Asterisk
// matches a REGISTER by endpoint id; pbx/domain/credential.go), and a
// person configuring a phone must be given it. It is therefore allowed in
// exactly one place on the wire — the documented authUsername field —
// and is stripped there before scanning.
const AUTH_USERNAME_FIELD = /"authUsername"\s*:\s*"[et]_[0-9a-f]{32}"/g;

export interface Finding {
  where: string;
  what: string;
}

export function engineFindings(where: string, text: string): Finding[] {
  const scanned = text.replace(AUTH_USERNAME_FIELD, '"authUsername":"<sip-username>"');
  return ENGINE_PATTERNS.filter((p) => p.pattern.test(scanned)).map((p) => ({
    where,
    what: `${p.name}: ${p.pattern.exec(scanned)?.[0] ?? ""}`,
  }));
}

export function secretFindings(where: string, text: string, secrets: Record<string, string>): Finding[] {
  return Object.entries(secrets)
    .filter(([, value]) => value.length >= 6 && text.includes(value))
    .map(([name]) => ({ where, what: `${name} present` }));
}

// renderedSurface is everything a person or assistive technology can get
// from the page: its text, and every attribute value (links, labels,
// titles, form values, data attributes).
export async function renderedSurface(page: Page): Promise<string> {
  return page.evaluate(() => {
    const parts: string[] = [document.title, document.body.innerText];
    for (const el of Array.from(document.querySelectorAll("*"))) {
      for (const attr of Array.from(el.attributes)) parts.push(attr.value);
      if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) parts.push(el.value);
    }
    return parts.join("\n");
  });
}

export interface Exchange {
  procedure: string;
  url: string;
  authorization: string | null;
  requestBody: string;
  responseBody: string;
}

// recordTraffic captures every public API exchange the page makes.
export function recordTraffic(page: Page): Exchange[] {
  const exchanges: Exchange[] = [];
  page.on("response", async (response: Response) => {
    const request: Request = response.request();
    const url = new URL(request.url());
    if (!url.pathname.startsWith("/atsapbx.v1.")) return;
    let responseBody = "";
    try {
      responseBody = await response.text();
    } catch {
      responseBody = "";
    }
    exchanges.push({
      procedure: url.pathname.slice(1),
      url: request.url(),
      authorization: request.headers()["authorization"] ?? null,
      requestBody: request.postData() ?? "",
      responseBody,
    });
  });
  return exchanges;
}

// Where each credential may legitimately travel: a password only to the
// procedures that take one, a licence key only to ApplyLicenseKey, and
// none of them ever back in a response, in a URL, or anywhere else.
const PASSWORD_PROCEDURES = new Set([
  "atsapbx.v1.AuthService/SignIn",
  "atsapbx.v1.AuthService/ChangePassword",
  "atsapbx.v1.AuthService/CompletePasswordReset",
]);
const LICENCE_KEY_PROCEDURES = new Set(["atsapbx.v1.LicensingService/ApplyLicenseKey"]);

export interface Credentials {
  password?: string;
  licenceKey?: string;
  refreshSecret?: string;
}

export function trafficFindings(exchanges: Exchange[], creds: Credentials): Finding[] {
  const findings: Finding[] = [];
  const tokens = new Set(exchanges.map((e) => e.authorization?.replace(/^Bearer /, "")).filter((t): t is string => !!t));
  for (const e of exchanges) {
    findings.push(...engineFindings(`${e.procedure} response`, e.responseBody));
    findings.push(...engineFindings(`${e.procedure} request`, e.requestBody));
    for (const t of tokens) {
      if (e.url.includes(t)) findings.push({ where: e.url, what: "access token in a URL" });
      if (e.requestBody.includes(t)) findings.push({ where: `${e.procedure} request`, what: "access token in a request body" });
    }
    if (creds.password) {
      if (!PASSWORD_PROCEDURES.has(e.procedure)) findings.push(...secretFindings(`${e.procedure} request`, e.requestBody, { password: creds.password }));
      findings.push(...secretFindings(`${e.procedure} response`, e.responseBody, { password: creds.password }));
      findings.push(...secretFindings(e.url, e.url, { password: creds.password }));
    }
    if (creds.licenceKey) {
      if (!LICENCE_KEY_PROCEDURES.has(e.procedure)) findings.push(...secretFindings(`${e.procedure} request`, e.requestBody, { licenceKey: creds.licenceKey }));
      findings.push(...secretFindings(`${e.procedure} response`, e.responseBody, { licenceKey: creds.licenceKey }));
    }
    if (creds.refreshSecret) {
      findings.push(...secretFindings(`${e.procedure} body`, e.requestBody + e.responseBody + e.url, { refreshSecret: creds.refreshSecret }));
    }
  }
  return findings;
}

// renderFindings scans the rendered surface for engine identifiers and
// for any credential of the session: the password, the licence key, the
// refresh secret, and every access token the page sent.
export function renderFindings(where: string, surface: string, exchanges: Exchange[], creds: Credentials): Finding[] {
  const secrets: Record<string, string> = {};
  if (creds.password) secrets.password = creds.password;
  if (creds.licenceKey) secrets.licenceKey = creds.licenceKey;
  if (creds.refreshSecret) secrets.refreshSecret = creds.refreshSecret;
  exchanges.forEach((e, i) => {
    const token = e.authorization?.replace(/^Bearer /, "");
    if (token) secrets[`access token #${i}`] = token;
  });
  return [...engineFindings(where, surface), ...secretFindings(where, surface, secrets)];
}
