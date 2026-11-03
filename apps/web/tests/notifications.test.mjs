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
const { notificationState, NotificationsView, disableNotifications } = settings;
const { applyBadge } = component("app/components/InboxCountProvider.js");
const render = props => renderToStaticMarkup(React.createElement(NotificationsView, props));

test("each of the five card states is detected from browser truth", () => {
  const base = { supported: true, ios: false, installed: false, serverEnabled: true, permission: "default", subscribed: false };
  assert.equal(notificationState({ ...base, supported: false }), "unsupported");
  assert.equal(notificationState({ ...base, serverEnabled: false }), "unsupported");
  assert.equal(notificationState({ ...base, permission: "denied" }), "denied");
  assert.equal(notificationState({ ...base, permission: "granted" }), "granted");
  assert.equal(notificationState({ ...base, permission: "granted", subscribed: true }), "subscribed");
  assert.equal(notificationState(base), "not-asked");
});

test("iOS outside the home screen is unsupported and asks for an install", () => {
  assert.equal(notificationState({ supported: true, ios: true, installed: false, serverEnabled: true, permission: "default", subscribed: false }), "unsupported");
  assert.equal(notificationState({ supported: true, ios: true, installed: true, serverEnabled: true, permission: "default", subscribed: false }), "not-asked");
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
  const html = render({ state: "subscribed" });
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

test("turning off says the server did not confirm when the delete fails", async () => {
  const serviceWorker = {
    getRegistration: async () => ({ pushManager: { getSubscription: async () => ({ endpoint: "https://push.example/device", unsubscribe: async () => {} }) } }),
  };
  assert.equal(await disableNotifications(serviceWorker, async () => ({ ok: false })), "unconfirmed");
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
