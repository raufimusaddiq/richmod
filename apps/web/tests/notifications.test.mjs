// The settings card is a pure view of browser state, so every state it can
// show is rendered here. The badge helper is the only place the icon badge is
// set or cleared.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import test from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import React from "react";
import swc from "next/dist/build/swc/index.js";

const require = createRequire(import.meta.url);
await swc.loadBindings();
// The page imports the shell for its own rendering; this file only renders the
// exported card, so the icon and router packages are replaced by inert stubs.
const stubs = {
  "@phosphor-icons/react": new Proxy({}, { get: () => () => null }),
  "next/link": { __esModule: true, default: ({ children }) => children },
  "next/navigation": { usePathname: () => "/settings" },
};
const cache = new Map();
function component(filename) {
  const absolute = path.resolve(filename);
  if (cache.has(absolute)) return cache.get(absolute).exports;
  const module = { exports: {} };
  cache.set(absolute, module);
  const { code } = swc.transformSync(fs.readFileSync(absolute, "utf8"), {
    filename: absolute, jsc: { parser: { syntax: "ecmascript", jsx: true }, transform: { react: { runtime: "automatic" } } }, module: { type: "commonjs" },
  });
  new Function("require", "module", "exports", code)(name => name.startsWith(".") ? component(path.resolve(path.dirname(absolute), name.endsWith(".js") ? name : `${name}.js`)) : stubs[name] || require(name), module, module.exports);
  return module.exports;
}

const settings = component("app/settings/page.js");
const { notificationState, reconcileSubscription, loadPushView, NotificationsView, registerSubscription, disableNotifications } = settings;
const { applyBadge } = component("app/components/InboxCountProvider.js");
const render = props => renderToStaticMarkup(React.createElement(NotificationsView, props));

test("each of the five card states is detected from the three facts", () => {
  const base = { supported: true, ios: false, installed: false, permission: "default", browserEndpoint: false, serverState: "absent", serverEnabled: "enabled" };
  assert.equal(notificationState({ ...base, supported: false }), "unsupported");
  assert.equal(notificationState({ ...base, permission: "denied" }), "denied");
  assert.equal(notificationState({ ...base, permission: "granted" }), "granted");
  assert.equal(notificationState({ ...base, permission: "granted", browserEndpoint: true, serverState: "live" }), "subscribed");
  assert.equal(notificationState(base), "not-asked");
  assert.equal(notificationState({ ...base, serverEnabled: "disabled" }), "unsupported", "no endpoint and no registrations");
  assert.equal(notificationState({ ...base, serverEnabled: "disabled", permission: "granted", browserEndpoint: true, serverState: "live" }), "subscribed", "E disabled does not hide a live device");
  assert.equal(notificationState({ ...base, serverEnabled: "unknown", permission: "granted" }), "granted", "E unknown is not E disabled");
});

test("a browser endpoint only counts as active when the server lists it live for this session", () => {
  const endpoint = "https://push.example/device";
  assert.deepEqual(reconcileSubscription({ browserEndpoint: endpoint, serverSubscriptions: [{ endpoint, active: true }] }), { serverState: "live", canDisable: true, issue: "" });
  assert.deepEqual(reconcileSubscription({ browserEndpoint: endpoint, serverSubscriptions: [{ endpoint, active: false }] }), { serverState: "retired", canDisable: true, issue: "retired" });
  assert.deepEqual(reconcileSubscription({ browserEndpoint: endpoint, serverSubscriptions: [{ endpoint: "https://push.example/other", active: true }] }), { serverState: "absent", canDisable: true, issue: "unregistered" }, "another account's row is not this session's row");
  assert.deepEqual(reconcileSubscription({ browserEndpoint: endpoint, serverSubscriptions: null }), { serverState: "unknown", canDisable: true, issue: "unknown" }, "an unknown server keeps the opt-out");
  assert.deepEqual(reconcileSubscription({ browserEndpoint: null, serverSubscriptions: [{ endpoint, active: true }] }), { serverState: "absent", canDisable: false, issue: "" });
});

test("an unregistered or retired endpoint is never painted active", () => {
  for (const issue of ["unregistered", "retired", "unknown"]) {
    const state = notificationState({ supported: true, ios: false, installed: false, permission: "granted", browserEndpoint: true, serverState: issue === "unregistered" ? "absent" : issue === "retired" ? "retired" : "unknown", serverEnabled: "enabled" });
    assert.equal(state, "granted");
    const html = render({ state, issue, canDisable: true });
    assert.match(html, /<button[^>]*>Coba lagi<\/button>/, `${issue} keeps the retry affordance`);
    assert.doesNotMatch(html, />Aktif</, `${issue} must not say Aktif`);
  }
  assert.match(render({ state: "granted", issue: "unregistered" }), /akun yang sedang masuk/);
  assert.match(render({ state: "granted", issue: "retired" }), /Server menonaktifkan/);
  assert.match(render({ state: "granted", issue: "unknown" }), /Server tidak dapat dihubungi/);
});

test("a 5xx registration POST keeps the endpoint and lets the server list decide", async () => {
  let unsubscribed = false;
  const subscription = {
    endpoint: "https://push.example/device",
    toJSON: () => ({ endpoint: "https://push.example/device", keys: { p256dh: "p", auth: "a" } }),
    unsubscribe: async () => { unsubscribed = true; },
  };
  const registration = { pushManager: { getSubscription: async () => subscription } };
  const serviceWorker = { getRegistration: async () => registration, register: async () => registration };
  const result = await registerSubscription(serviceWorker, "BEl6", async () => ({ ok: false, status: 503 }));
  assert.equal(result.status, "unconfirmed");
  assert.equal(unsubscribed, false, "a 5xx may have committed; the endpoint must survive");
  const request = async url => url === "/api/v1/push/public-key"
    ? { ok: true, json: async () => ({ enabled: true, publicKey: "BEl6" }) }
    : { ok: true, json: async () => ({ subscriptions: [{ endpoint: subscription.endpoint, active: true }] }) };
  const view = await loadPushView({ supported: true, ios: false, installed: false, permission: "granted", serviceWorker, request });
  assert.equal(view.state, "subscribed", "the next refresh reads the server list, not the browser alone");
  assert.equal(view.canDisable, true);
});

test("a 4xx on a pre-existing endpoint keeps it, because the server may still hold the row", async () => {
  let unsubscribed = false;
  const subscription = {
    endpoint: "https://push.example/device",
    toJSON: () => ({ endpoint: "https://push.example/device", keys: { p256dh: "p", auth: "a" } }),
    unsubscribe: async () => { unsubscribed = true; },
  };
  const registration = { pushManager: { getSubscription: async () => subscription } };
  const serviceWorker = { getRegistration: async () => registration, register: async () => registration };
  const result = await registerSubscription(serviceWorker, "BEl6", async () => ({ ok: false, status: 401 }));
  assert.equal(result.status, "unconfirmed");
  assert.equal(unsubscribed, false, "a pre-existing endpoint is never discarded on a rejection");
});

test("a 4xx on an endpoint created in this call is a definitive refusal", async () => {
  let unsubscribed = false;
  const subscription = {
    endpoint: "https://push.example/device",
    toJSON: () => ({ endpoint: "https://push.example/device", keys: { p256dh: "p", auth: "a" } }),
    unsubscribe: async () => { unsubscribed = true; },
  };
  const registration = { pushManager: { getSubscription: async () => null, subscribe: async () => subscription } };
  const serviceWorker = { getRegistration: async () => registration, register: async () => registration };
  const result = await registerSubscription(serviceWorker, "BEl6", async () => ({ ok: false, status: 409 }));
  assert.equal(result.status, "rejected");
  assert.equal(unsubscribed, true);
});

test("an unreachable server does not disable push and keeps the opt-out", async () => {
  const subscription = { endpoint: "https://push.example/device" };
  const serviceWorker = { getRegistration: async () => ({ pushManager: { getSubscription: async () => subscription } }) };
  const request = async () => { throw new TypeError("offline"); };
  const view = await loadPushView({ supported: true, ios: false, installed: false, permission: "granted", serviceWorker, request });
  assert.equal(view.serverDisabled, false, "an unknown server is not asserted as disabled");
  assert.equal(view.serverUnavailable, true);
  assert.equal(view.canDisable, true);
  assert.equal(view.state, "granted");
  const html = render(view);
  assert.match(html, /<button[^>]*>Matikan notifikasi<\/button>/, "the opt-out survives an unreachable server");
  assert.doesNotMatch(html, /Server ini belum mengaktifkan notifikasi push/);
  assert.match(html, /Server tidak dapat dihubungi/);
});

test("only a definitive key answer asserts push is disabled", async () => {
  const serviceWorker = { getRegistration: async () => undefined };
  const request = async url => url === "/api/v1/push/public-key"
    ? { ok: false, status: 404 }
    : { ok: true, json: async () => ({ subscriptions: [] }) };
  const view = await loadPushView({ supported: true, ios: false, installed: false, permission: "default", serviceWorker, request });
  assert.equal(view.serverDisabled, true);
  assert.equal(view.serverUnavailable, false);
  assert.equal(view.state, "unsupported");
});

test("a definitive disabled key still wins when the subscription list is unavailable", async () => {
  const serviceWorker = { getRegistration: async () => undefined };
  const request = async url => {
    if (url === "/api/v1/push/public-key") return { ok: true, json: async () => ({ enabled: false }) };
    throw new TypeError("offline");
  };
  const view = await loadPushView({ supported: true, ios: false, installed: false, permission: "default", serviceWorker, request });
  assert.equal(view.serverDisabled, true);
  assert.equal(view.serverUnavailable, true);
  assert.equal(view.state, "unsupported");
});

test("a 503 key answer is unavailable, not disabled", async () => {
  const serviceWorker = { getRegistration: async () => undefined };
  const request = async url => url === "/api/v1/push/public-key"
    ? { ok: false, status: 503 }
    : { ok: true, json: async () => ({ subscriptions: [] }) };
  const view = await loadPushView({ supported: true, ios: false, installed: false, permission: "default", serviceWorker, request });
  assert.equal(view.serverDisabled, false);
  assert.equal(view.serverUnavailable, true);
  assert.notEqual(view.state, "unsupported");
});

test("a disabled server still shows a live device and its opt-out", async () => {
  const endpoint = "https://push.example/device";
  const serviceWorker = { getRegistration: async () => ({ pushManager: { getSubscription: async () => ({ endpoint }) } }) };
  const request = async url => url === "/api/v1/push/public-key"
    ? { ok: true, json: async () => ({ enabled: false }) }
    : { ok: true, json: async () => ({ subscriptions: [{ endpoint, active: true }] }) };
  const view = await loadPushView({ supported: true, ios: false, installed: false, permission: "granted", serviceWorker, request });
  assert.equal(view.serverDisabled, true);
  assert.equal(view.state, "subscribed", "a live device is not collapsed into unsupported");
  assert.equal(view.canDisable, true);
  const html = render(view);
  assert.match(html, /<button[^>]*>Matikan notifikasi<\/button>/);
  assert.match(html, /tetap dapat dimatikan/);
  assert.doesNotMatch(html, /belum terdaftar|tidak aktif/i);
});

test("an endpoint owned by another account is replaced, not retried", async () => {
  const oldEndpoint = "https://push.example/old-device";
  const newEndpoint = "https://push.example/new-device";
  let unsubscribed = 0;
  let subscribed = 0;
  const oldSubscription = {
    endpoint: oldEndpoint,
    toJSON: () => ({ endpoint: oldEndpoint, keys: { p256dh: "old", auth: "old" } }),
    unsubscribe: async () => { unsubscribed += 1; },
  };
  const newSubscription = {
    endpoint: newEndpoint,
    toJSON: () => ({ endpoint: newEndpoint, keys: { p256dh: "new", auth: "new" } }),
  };
  const registration = {
    pushManager: {
      getSubscription: async () => oldSubscription,
      subscribe: async () => { subscribed += 1; return newSubscription; },
    },
  };
  const serviceWorker = { getRegistration: async () => registration, register: async () => registration };
  const calls = [];
  const request = async (url, options) => {
    calls.push({ url, options });
    if (calls.length === 1) return { ok: false, status: 409, json: async () => ({ code: "ENDPOINT_OWNED_BY_ANOTHER_USER" }) };
    return { ok: true, json: async () => ({}) };
  };
  const result = await registerSubscription(serviceWorker, "BEl6", request);
  assert.equal(result.status, "registered");
  assert.equal(result.replacedEndpoint, oldEndpoint);
  assert.equal(unsubscribed, 1, "the old browser subscription is removed once");
  assert.equal(subscribed, 1, "a new browser subscription is created once");
  assert.equal(calls.length, 2);
  assert.equal(JSON.parse(calls[1].options.body).endpoint, newEndpoint, "the second POST carries the new endpoint");
  assert.deepEqual(JSON.parse(calls[1].options.body).keys, { p256dh: "new", auth: "new" });
});

test("a thrown registration POST keeps the endpoint and reports unconfirmed", async () => {
  let unsubscribed = false;
  const subscription = {
    endpoint: "https://push.example/device",
    toJSON: () => ({ endpoint: "https://push.example/device", keys: { p256dh: "p", auth: "a" } }),
    unsubscribe: async () => { unsubscribed = true; },
  };
  const registration = { pushManager: { getSubscription: async () => subscription } };
  const serviceWorker = { getRegistration: async () => registration, register: async () => registration };
  const result = await registerSubscription(serviceWorker, "BEl6", async () => { throw new TypeError("network down"); });
  assert.equal(result.status, "unconfirmed");
  assert.equal(unsubscribed, false, "an unknown outcome must not discard the endpoint");
});

test("a confirmed registration posts this endpoint and keys without a household", async () => {
  const calls = [];
  const subscription = {
    endpoint: "https://push.example/device",
    toJSON: () => ({ endpoint: "https://push.example/device", keys: { p256dh: "p", auth: "a" } }),
    unsubscribe: async () => { throw new Error("must not unsubscribe"); },
  };
  const registration = { pushManager: { getSubscription: async () => subscription } };
  const serviceWorker = { getRegistration: async () => registration, register: async () => registration };
  const result = await registerSubscription(serviceWorker, "BEl6", async (url, options) => { calls.push({ url, options }); return { ok: true }; });
  assert.equal(result.status, "registered");
  assert.equal(calls[0].url, "/api/v1/push/subscriptions");
  assert.equal(calls[0].options.method, "POST");
  assert.deepEqual(JSON.parse(calls[0].options.body), { endpoint: "https://push.example/device", keys: { p256dh: "p", auth: "a" } });
});

test("iOS outside the home screen is unsupported and asks for an install", () => {
  const base = { supported: true, ios: true, permission: "default", browserEndpoint: false, serverState: "absent", serverEnabled: "enabled" };
  assert.equal(notificationState({ ...base, installed: false }), "unsupported");
  assert.equal(notificationState({ ...base, installed: true }), "not-asked");
  const html = render({ state: "unsupported", iosNotInstalled: true });
  assert.match(html, /Layar Utama/);
  assert.match(html, /Tambahkan ke Layar Utama/);
  assert.doesNotMatch(html, /<button/, "no button is offered where push cannot work");
});

test("unsupported for an unconfigured server says so and offers no button", () => {
  const html = render({ state: "unsupported", serverDisabled: true });
  assert.match(html, /Server ini belum mengaktifkan notifikasi push/);
  assert.doesNotMatch(html, /<button/);
});

test("denied explains browser settings and never offers a request button", () => {
  const html = render({ state: "denied" });
  assert.match(html, /Diblokir/);
  assert.match(html, /pengaturan situs di peramban/);
  assert.doesNotMatch(html, /<button/);
});

test("not asked offers the only permission request", () => {
  const html = render({ state: "not-asked" });
  assert.match(html, /Belum diaktifkan/);
  assert.match(html, /<button[^>]*>Aktifkan notifikasi<\/button>/);
});

test("granted but not subscribed offers a retry", () => {
  const html = render({ state: "granted" });
  assert.match(html, /Belum terdaftar/);
  assert.match(html, /<button[^>]*>Coba lagi<\/button>/);
});

test("subscribed offers turning off", () => {
  const html = render({ state: "subscribed", canDisable: true });
  assert.match(html, /Aktif/);
  assert.match(html, /<button[^>]*>Matikan notifikasi<\/button>/);
});

for (const state of ["unsupported", "not-asked", "denied", "granted", "subscribed"]) {
  test(`the ${state} state announces itself in a live region`, () => {
    const html = render({ state, status: "Notifikasi aktif di perangkat ini." });
    assert.match(html, /role="status" aria-live="polite"/);
    assert.match(html, /Notifikasi aktif di perangkat ini\./);
    assert.doesNotMatch(html, /class="health-dot/, "status is text, not colour alone");
  });
}

test("turning off both deletes the server row and unsubscribes locally", async () => {
  const calls = [];
  let unsubscribed = false;
  const serviceWorker = {
    getRegistration: async () => ({ pushManager: { getSubscription: async () => ({ endpoint: "https://push.example/device", unsubscribe: async () => { unsubscribed = true; } }) } }),
  };
  const result = await disableNotifications(serviceWorker, async (url, options) => {
    calls.push({ url, options });
    return { ok: true };
  });
  assert.equal(result, "off");
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, "/api/v1/push/subscriptions");
  assert.equal(calls[0].options.method, "DELETE");
  assert.deepEqual(JSON.parse(calls[0].options.body), { endpoint: "https://push.example/device" });
  assert.equal(unsubscribed, true, "the local subscription is removed too");
});

test("a failed delete keeps the endpoint so it can be retried", async () => {
  let unsubscribed = false;
  const serviceWorker = {
    getRegistration: async () => ({ pushManager: { getSubscription: async () => ({ endpoint: "https://push.example/device", unsubscribe: async () => { unsubscribed = true; } }) } }),
  };
  assert.equal(await disableNotifications(serviceWorker, async () => ({ ok: false })), "unconfirmed");
  assert.equal(unsubscribed, false, "the endpoint survives an unconfirmed delete");
});

test("a thrown delete keeps the endpoint and reports an error", async () => {
  let unsubscribed = false;
  const serviceWorker = {
    getRegistration: async () => ({ pushManager: { getSubscription: async () => ({ endpoint: "https://push.example/device", unsubscribe: async () => { unsubscribed = true; } }) } }),
  };
  assert.equal(await disableNotifications(serviceWorker, async () => { throw new TypeError("network down"); }), "error");
  assert.equal(unsubscribed, false, "the endpoint survives a network failure");
});

test("turning off does not call the server when this device holds no subscription", async () => {
  let requests = 0;
  const serviceWorker = { getRegistration: async () => ({ pushManager: { getSubscription: async () => null } }) };
  assert.equal(await disableNotifications(serviceWorker, async () => { requests += 1; return { ok: true }; }), "missing");
  assert.equal(requests, 0);
});

test("the icon badge mirrors the count and clears at zero", () => {
  const calls = [];
  applyBadge({ setAppBadge: async value => calls.push(["set", value]), clearAppBadge: async () => calls.push(["clear"]) }, 4);
  applyBadge({ setAppBadge: async value => calls.push(["set", value]), clearAppBadge: async () => calls.push(["clear"]) }, 0);
  return Promise.resolve().then(() => assert.deepEqual(calls, [["set", 4], ["clear"]]));
});

test("the icon badge is a no-op where the Badging API does not exist", () => {
  assert.doesNotThrow(() => applyBadge({}, 3));
  assert.doesNotThrow(() => applyBadge(null, 3));
  let calls = 0;
  assert.doesNotThrow(() => applyBadge({ setAppBadge: () => { calls += 1; throw new Error("denied"); } }, 1));
  assert.equal(calls, 1, "a synchronously throwing badge call is contained");
});
