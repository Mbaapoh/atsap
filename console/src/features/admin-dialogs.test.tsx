import { screen, within } from "@testing-library/react";
import userEvent, { type UserEvent } from "@testing-library/user-event";
import { http, HttpResponse } from "msw";
import { describe, expect, it } from "vitest";
import { routes } from "../app/routes";
import { OVERVIEW_URLS, server } from "../test/msw";
import { renderApp } from "../test/session";

// console-admin-screens: every admin dialog answers every refusal with
// console copy, never the server's words (console brief §6 rule 10) — the
// server message here is deliberately full of engine and database detail —
// and every one can be left without sending anything.

const HOSTILE = "pq: ps_endpoints e_0123456789abcdef0123456789abcdef in pjsip.conf";
const STATUS: Record<string, number> = { permission_denied: 403, not_found: 404, already_exists: 409, failed_precondition: 400, invalid_argument: 400, internal: 500 };
const rpc = (p: string) => `http://localhost:3000/atsapbx.v1.${p}`;

const ben = { principal: { principalId: "p-ben", email: "ben@example.com", displayName: "Ben Tabi", status: "PRINCIPAL_STATUS_ACTIVE" }, roleBindings: [{ role: "SUPERVISOR", scope: "tenant" }] };
const ext = { extensionId: "x1", number: "1001", displayName: "Reception", deviceType: "DEVICE_TYPE_SIP" };
const trunk = { trunkId: "t1", name: "Carrier A", host: "sip.carrier-a.example", channelLimit: 30, priority: 1 };
const route = { routeId: "r1", prefixPattern: "+237", trunkId: "t1", priority: 1 };

const lists = [
  http.post(OVERVIEW_URLS.principals, () => HttpResponse.json({ principals: [ben] })),
  http.post(OVERVIEW_URLS.extensions, () => HttpResponse.json({ extensions: [ext] })),
  http.post(OVERVIEW_URLS.trunks, () => HttpResponse.json({ trunks: [trunk] })),
  http.post(OVERVIEW_URLS.routes, () => HttpResponse.json({ routes: [route] })),
];

async function menu(user: UserEvent, actions: string, item: string) {
  await user.click(await screen.findByRole("button", { name: `Actions for ${actions}` }));
  await user.click(screen.getByRole("menuitem", { name: item }));
}
const dialog = () => within(screen.getAllByRole("dialog").at(-1)!);
const press = (user: UserEvent, name: string) => user.click(dialog().getByRole("button", { name }));

interface Flow {
  path: string;
  procedure: string;
  run: (user: UserEvent) => Promise<void>;
}

const flows: Record<string, Flow> = {
  "add user": {
    path: "/users",
    procedure: "IdentityService/ProvisionPrincipal",
    run: async (user) => {
      await user.click(await screen.findByRole("button", { name: "Add user" }));
      await user.type(dialog().getByLabelText("Email"), "new@example.com");
      await user.type(dialog().getByLabelText("Name"), "New Person");
      await press(user, "Add user");
    },
  },
  "grant role": {
    path: "/users",
    procedure: "IdentityService/GrantRole",
    run: async (user) => {
      await menu(user, "Ben Tabi", "Grant a role…");
      await press(user, "Grant role");
    },
  },
  "revoke role": {
    path: "/users",
    procedure: "IdentityService/RevokeRole",
    run: async (user) => {
      await menu(user, "Ben Tabi", "Revoke Supervisor");
      await press(user, "Revoke role");
    },
  },
  disable: {
    path: "/users",
    procedure: "IdentityService/SetPrincipalStatus",
    run: async (user) => {
      await menu(user, "Ben Tabi", "Disable…");
      await press(user, "Disable");
    },
  },
  "reset password": {
    path: "/users",
    procedure: "IdentityService/ResetPrincipalPassword",
    run: async (user) => {
      await menu(user, "Ben Tabi", "Reset password…");
      await press(user, "Reset password");
    },
  },
  "add extension": {
    path: "/extensions",
    procedure: "PbxService/CreateExtension",
    run: async (user) => {
      await user.click(await screen.findByRole("button", { name: "Add extension" }));
      await user.type(dialog().getByLabelText("Number"), "1009");
      await user.type(dialog().getByLabelText("Name"), "Spare");
      await press(user, "Add extension");
    },
  },
  "edit extension": {
    path: "/extensions",
    procedure: "PbxService/UpdateExtension",
    run: async (user) => {
      await menu(user, "extension 1001", "Edit…");
      await press(user, "Save changes");
    },
  },
  "rotate extension": {
    path: "/extensions",
    procedure: "PbxService/RegenerateSecret",
    run: async (user) => {
      await menu(user, "extension 1001", "Rotate SIP password…");
      await press(user, "Rotate password");
    },
  },
  "delete extension": {
    path: "/extensions",
    procedure: "PbxService/DeleteExtension",
    run: async (user) => {
      await menu(user, "extension 1001", "Delete extension…");
      await press(user, "Delete extension");
    },
  },
  "add trunk": {
    path: "/trunks",
    procedure: "PbxService/CreateTrunk",
    run: async (user) => {
      await user.click(await screen.findByRole("button", { name: "Add trunk" }));
      await user.type(dialog().getByLabelText("Name"), "Carrier C");
      await user.type(dialog().getByLabelText("Host"), "carrier-c.example");
      await user.type(dialog().getByLabelText("Channel limit"), "4");
      await press(user, "Add trunk");
    },
  },
  "edit trunk": {
    path: "/trunks",
    procedure: "PbxService/UpdateTrunk",
    run: async (user) => {
      await menu(user, "trunk Carrier A", "Edit…");
      await press(user, "Save changes");
    },
  },
  "delete trunk": {
    path: "/trunks",
    procedure: "PbxService/DeleteTrunk",
    run: async (user) => {
      await menu(user, "trunk Carrier A", "Delete trunk…");
      await press(user, "Delete trunk");
    },
  },
  "add route": {
    path: "/trunks",
    procedure: "PbxService/CreateRoute",
    run: async (user) => {
      await screen.findByRole("table", { name: "Outbound routes of this tenant" });
      await user.click(screen.getByRole("button", { name: "Add route" }));
      await user.type(dialog().getByLabelText("Prefix"), "+44");
      await press(user, "Add route");
    },
  },
  "replace route": {
    path: "/trunks",
    procedure: "PbxService/CreateRoute",
    run: async (user) => {
      await menu(user, "route +237", "Replace…");
      await press(user, "Continue");
      await press(user, "Replace route");
    },
  },
  "delete route": {
    path: "/trunks",
    procedure: "PbxService/DeleteRoute",
    run: async (user) => {
      await menu(user, "route +237", "Delete route…");
      await press(user, "Delete route");
    },
  },
};

// What each dialog says, per refusal. A kind a dialog has no words of its
// own for falls back to the console's fixed copy for that kind.
const expected: [string, string, string | RegExp][] = [
  ["add user", "permission_denied", "Your role cannot add people to this tenant."],
  ["add user", "invalid_argument", "Some details are not valid. Check the email address and password."],
  ["add user", "internal", /Something went wrong/],
  ["grant role", "permission_denied", "Your role cannot grant that role."],
  ["grant role", "internal", /Something went wrong/],
  ["revoke role", "permission_denied", "Your role cannot revoke that role."],
  ["revoke role", "internal", /Something went wrong/],
  ["disable", "permission_denied", "Your role cannot disable people in this tenant."],
  ["disable", "failed_precondition", "Ben Tabi is this tenant's last active administrator and cannot be disabled."],
  ["disable", "internal", /Something went wrong/],
  ["reset password", "internal", /Something went wrong/],
  ["add extension", "permission_denied", "Your role cannot add extensions."],
  ["add extension", "invalid_argument", "Some details are not valid. Check the number and name."],
  ["add extension", "internal", /Something went wrong/],
  ["edit extension", "permission_denied", "Your role cannot change extensions."],
  ["edit extension", "not_found", "This extension no longer exists. It may have been removed."],
  ["edit extension", "internal", /Something went wrong/],
  ["rotate extension", "permission_denied", "Your role cannot rotate passwords of extensions."],
  ["rotate extension", "internal", /Something went wrong/],
  ["delete extension", "permission_denied", "Your role cannot delete extensions."],
  ["delete extension", "internal", /Something went wrong/],
  ["add trunk", "invalid_argument", "Some details are not valid. Check the host, port and username."],
  ["add trunk", "already_exists", "A trunk with that name already exists."],
  ["add trunk", "permission_denied", "Your role cannot change trunks."],
  ["edit trunk", "internal", /Something went wrong/],
  ["delete trunk", "permission_denied", /Your role cannot/],
  ["delete trunk", "internal", /Something went wrong/],
  ["add route", "permission_denied", "Your role cannot change routes."],
  ["add route", "invalid_argument", "Some details are not valid. Check the prefix, trunk and cost."],
  ["add route", "not_found", "That trunk or route no longer exists. Reload and try again."],
  ["replace route", "internal", /Something went wrong/],
  ["delete route", "permission_denied", "Your role cannot change routes."],
  ["delete route", "internal", /Something went wrong/],
];

describe("admin dialogs refused by the server", () => {
  it.each(expected)("%s, refused with %s, says it in console words", async (flow, code, text) => {
    const { path, procedure, run } = flows[flow]!;
    server.use(...lists, http.post(rpc(procedure), () => HttpResponse.json({ code, message: HOSTILE }, { status: STATUS[code] })));
    const user = userEvent.setup();
    renderApp(routes, path);
    await run(user);
    expect(await screen.findByText(text)).toBeInTheDocument();
    expect(screen.getAllByRole("dialog").length).toBeGreaterThan(0); // the dialog stays open to retry or cancel
    expect(document.body.textContent).not.toMatch(/pq:|ps_endpoints|pjsip|e_[0-9a-f]{32}/);
  });
});

// How each dialog is opened, and the button that leaves it unsent.
const openers: [string, string, (user: UserEvent) => Promise<void>, string][] = [
  ["add user", "/users", (u) => u.click(screen.getByRole("button", { name: "Add user" })), "Cancel"],
  ["grant role", "/users", (u) => menu(u, "Ben Tabi", "Grant a role…"), "Cancel"],
  ["revoke role", "/users", (u) => menu(u, "Ben Tabi", "Revoke Supervisor"), "Cancel"],
  ["disable", "/users", (u) => menu(u, "Ben Tabi", "Disable…"), "Cancel"],
  ["reset password", "/users", (u) => menu(u, "Ben Tabi", "Reset password…"), "Cancel"],
  ["add extension", "/extensions", (u) => u.click(screen.getByRole("button", { name: "Add extension" })), "Cancel"],
  ["edit extension", "/extensions", (u) => menu(u, "extension 1001", "Edit…"), "Cancel"],
  ["rotate extension", "/extensions", (u) => menu(u, "extension 1001", "Rotate SIP password…"), "Cancel"],
  ["delete extension", "/extensions", (u) => menu(u, "extension 1001", "Delete extension…"), "Cancel"],
  ["add trunk", "/trunks", (u) => u.click(screen.getByRole("button", { name: "Add trunk" })), "Cancel"],
  ["delete trunk", "/trunks", (u) => menu(u, "trunk Carrier A", "Delete trunk…"), "Cancel"],
  ["add route", "/trunks", (u) => u.click(screen.getByRole("button", { name: "Add route" })), "Cancel"],
  ["delete route", "/trunks", (u) => menu(u, "route +237", "Delete route…"), "Cancel"],
];

describe("admin dialogs left without saving", () => {
  it.each(openers)("%s closes on Cancel and on Escape, sending nothing", async (_name, path, open, cancel) => {
    const sent: string[] = [];
    server.use(...lists, http.post(/atsapbx\.v1\.\w+\/(?!List|Get)\w+$/, ({ request }) => {
      sent.push(new URL(request.url).pathname);
      return HttpResponse.json({});
    }));
    const user = userEvent.setup();
    renderApp(routes, path);
    await screen.findAllByRole("table");
    await open(user);
    await press(user, cancel);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    await open(user);
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(sent).toEqual([]);
  });

  it("backing out of a replace returns to the form, unchanged", async () => {
    server.use(...lists);
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    await menu(user, "route +237", "Replace…");
    await user.clear(dialog().getByLabelText("Priority"));
    await user.type(dialog().getByLabelText("Priority"), "7");
    await press(user, "Continue");
    await press(user, "Cancel");
    expect(screen.getByRole("dialog", { name: "Replace route +237" })).toBeInTheDocument();
    expect(dialog().getByLabelText("Priority")).toHaveValue("7");
  });
});

describe("trunk and route forms check before sending", () => {
  it.each([
    ["Name", "x".repeat(101), /at most 100 characters/],
    ["Port (optional)", "70000", /port from 1 to 65535/],
    ["Channel limit", "0", /1 or more/],
    ["Priority", "-1", /0 or more/],
    ["Username (optional)", "acct 9", /letters, digits/],
  ])("trunk %s = %s is refused", async (label, value, message) => {
    let sent = 0;
    server.use(...lists, http.post(rpc("PbxService/CreateTrunk"), () => {
      sent++;
      return HttpResponse.json({});
    }));
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    await user.click(await screen.findByRole("button", { name: "Add trunk" }));
    await user.type(dialog().getByLabelText("Name"), "Carrier C");
    await user.type(dialog().getByLabelText("Host"), "carrier-c.example");
    await user.type(dialog().getByLabelText("Channel limit"), "4");
    await user.clear(dialog().getByLabelText(label));
    await user.click(dialog().getByLabelText(label));
    await user.paste(value);
    await press(user, "Add trunk");
    expect(dialog().getByLabelText(label)).toHaveAccessibleDescription(message);
    expect(sent).toBe(0);
  });

  it("a trunk secret longer than 255 characters is refused", async () => {
    server.use(...lists);
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    await user.click(await screen.findByRole("button", { name: "Add trunk" }));
    await user.click(dialog().getByLabelText("Username (optional)"));
    await user.paste("acct-1");
    await user.click(dialog().getByLabelText("Secret (optional)"));
    await user.paste("s".repeat(256));
    await press(user, "Add trunk");
    expect(dialog().getByLabelText("Secret (optional)")).toHaveAccessibleDescription(/at most 255 characters/);
  });

  it("a route priority below 0 is refused", async () => {
    server.use(...lists);
    const user = userEvent.setup();
    renderApp(routes, "/trunks");
    await screen.findByRole("table", { name: "Outbound routes of this tenant" });
    await user.click(screen.getByRole("button", { name: "Add route" }));
    await user.type(dialog().getByLabelText("Prefix"), "+44");
    await user.clear(dialog().getByLabelText("Priority"));
    await user.type(dialog().getByLabelText("Priority"), "-2");
    await press(user, "Add route");
    expect(dialog().getByLabelText("Priority")).toHaveAccessibleDescription(/0 or more/);
  });
});
