// The onboarding checklist is a pure view of the server's step states, so every
// state combination it can show is rendered here.
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

const { EmailOnboardingChecklist } = component("app/settings/page.js");
const render = props => renderToStaticMarkup(React.createElement(EmailOnboardingChecklist, props));
const states = html => Object.fromEntries([...html.matchAll(/data-step="(\w+)" data-state="(\w+)"/g)].map(match => [match[1], match[2]]));
const ingress = (steps, extra = {}) => ({ address: "h_abc@richmod.link", status: "PROVISIONED", forwardingAction: null, steps, ...extra });

test("renders nothing until the server reports step states", () => {
  assert.equal(render({ ingress: null, owner: true }), "");
  assert.equal(render({ ingress: { address: null }, owner: true }), "");
});

test("no address: only creating the address is current, the rest wait", () => {
  const html = render({ ingress: ingress({ address: "current", sender: "blocked", forwarding: "blocked", activated: "blocked" }, { address: null, status: null }), owner: true });
  assert.deepEqual(states(html), { address: "current", sender: "blocked", forwarding: "blocked", activated: "blocked" });
  assert.match(html, />Buat alamat<\/button>/);
  assert.doesNotMatch(html, />Aktifkan<\/button>/, "activation is not offered while blocked");
  assert.match(html, /aria-current="step"/);
  for (const title of ["Buat alamat", "Tambah pengirim", "Verifikasi penerusan", "Aktifkan"]) assert.match(html, new RegExp(`<b>${title}</b>`));
});

test("address without sender: sender is current, activation stays blocked with a reason", () => {
  const html = render({ ingress: ingress({ address: "done", sender: "current", forwarding: "current", activated: "blocked" }), owner: true });
  assert.deepEqual(states(html), { address: "done", sender: "current", forwarding: "current", activated: "blocked" });
  assert.match(html, /href="#notifikasi-bank"/);
  assert.match(html, /href="#provider-finansial"/);
  assert.match(html, /Tambahkan pengirim terlebih dahulu\./);
  assert.match(html, /h_abc@richmod\.link/);
  assert.doesNotMatch(html, />Aktifkan<\/button>/);
});

test("sender added: activation becomes the actionable step", () => {
  const html = render({ ingress: ingress({ address: "done", sender: "done", forwarding: "current", activated: "current" }), owner: true });
  assert.deepEqual(states(html), { address: "done", sender: "done", forwarding: "current", activated: "current" });
  assert.match(html, />Aktifkan<\/button>/);
  assert.doesNotMatch(html, /href="#notifikasi-bank"/, "a finished step has no action");
});

test("everything done after activation", () => {
  const html = render({ ingress: ingress({ address: "done", sender: "done", forwarding: "done", activated: "done" }, { status: "ACTIVE" }), owner: true });
  assert.deepEqual(states(html), { address: "done", sender: "done", forwarding: "done", activated: "done" });
  assert.doesNotMatch(html, /<button[^>]*>(Buat alamat|Aktifkan)<\/button>/);
  assert.doesNotMatch(html, /aria-current/);
});

test("the Gmail confirmation code and link appear inline for owners only", () => {
  const forwardingAction = { id: "action-1", title: "Konfirmasi penerusan Gmail", actionUrl: "https://mail-settings.google.com/mail/vf-abc", actionCode: "123456789" };
  const props = { ingress: ingress({ address: "done", sender: "done", forwarding: "current", activated: "current" }, { forwardingAction }) };
  const owner = render({ ...props, owner: true });
  assert.match(owner, /123456789/);
  assert.match(owner, /href="https:\/\/mail-settings\.google\.com\/mail\/vf-abc"[^>]*rel="noopener noreferrer"/);
  assert.match(owner, /Tandai selesai/);
  const member = render({ ...props, owner: false });
  assert.doesNotMatch(member, /123456789|mail-settings|Tandai selesai/);
});

test("members see progress but no actions", () => {
  const html = render({ ingress: ingress({ address: "done", sender: "current", forwarding: "current", activated: "blocked" }), owner: false });
  assert.deepEqual(states(html), { address: "done", sender: "current", forwarding: "current", activated: "blocked" });
  assert.doesNotMatch(html, /href="#notifikasi-bank"|>Aktifkan<\/button>|>Buat alamat<\/button>/);
});

test("rotation warns that forwarding rules must be redone; activation errors map to next steps", () => {
  const source = fs.readFileSync("app/settings/page.js", "utf8");
  assert.match(source, /ROTATE_CONFIRM = "[^"]*[Aa]turan penerusan[^"]*dibuat ulang/);
  assert.match(source, /confirm\(ROTATE_CONFIRM,/);
  assert.match(source, /SENDER_REQUIRED: "Tambahkan minimal satu pengirim/);
  assert.match(source, /ACTIVATE_ERROR_TEXT\[body\?\.code\]/);
  assert.match(source, /"active sender already exists": "[^"]+"/, "duplicate sender gets plain-language copy");
});
