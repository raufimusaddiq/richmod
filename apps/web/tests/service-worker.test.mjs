// Runs the real service worker in a VM. The point of this file is that there
// is no caching path left to test: a chunk request is never intercepted, and a
// push with a valid, empty, or malformed body all show a notification.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import test from "node:test";

const origin = "https://richmod.test";
const source = readFileSync(new URL("../public/sw.js", import.meta.url), "utf8");

function createWorker({ keys = [], windows = [] } = {}) {
  const listeners = {};
  const deleted = [];
  const shown = [];
  const opened = [];
  let claimed = false;
  const sandbox = {
    self: {
      addEventListener: (type, handler) => { listeners[type] = handler; },
      skipWaiting: () => {},
      location: { origin },
      registration: { showNotification: async (title, options) => { shown.push({ title, options }); } },
      clients: {
        claim: async () => { claimed = true; },
        matchAll: async () => windows,
        openWindow: async url => { opened.push(url); return { url }; },
      },
    },
    caches: {
      keys: async () => keys,
      delete: async key => { deleted.push(key); return true; },
    },
    Request,
    Response,
    URL,
  };
  runInNewContext(source, sandbox);
  async function dispatch(type, event) {
    event.waitUntil = promise => { event.done = promise; };
    listeners[type](event);
    await event.done;
  }
  return {
    listeners,
    deleted,
    shown,
    opened,
    isClaimed: () => claimed,
    dispatch,
    async activate() {
      const event = { waitUntil(promise) { event.done = promise; } };
      listeners.activate(event);
      await event.done;
    },
  };
}

const click = (url, overrides = {}) => ({
  notification: { close() { this.closed = true; }, data: url === undefined ? {} : { url } },
  ...overrides,
});

test("a chunk fetch is never intercepted and respondWith is never called", () => {
  const worker = createWorker();
  assert.equal(worker.listeners.fetch, undefined, "the worker registers no fetch handler");
  let intercepted = false;
  // If a fetch event were dispatched anyway, nothing would answer it; the
  // browser's own network request proceeds unchanged.
  worker.listeners.fetch?.({
    request: new Request("https://richmod.test/_next/static/chunks/app.js"),
    respondWith() { intercepted = true; },
  });
  assert.equal(intercepted, false);
});

test("activation deletes only the abandoned cache stores and opens no new one", async () => {
  const worker = createWorker({ keys: ["richmod-static-abc123", "richmod-generations", "unrelated"] });
  await worker.activate();
  assert.deepEqual(worker.deleted, ["richmod-static-abc123", "richmod-generations"]);
  assert.equal(worker.isClaimed(), true, "the pass-through worker still takes control");
  assert.doesNotMatch(source, /caches\.open/, "push listeners must not grow into a cache");
});

test("a valid push payload is shown with its own title, body, and target", async () => {
  const worker = createWorker();
  await worker.dispatch("push", { data: { json: async () => ({ title: "Review baru", body: "2 transaksi menunggu tinjauan.", url: "/inbox", count: 2 }) } });
  assert.equal(worker.shown.length, 1);
  assert.equal(worker.shown[0].title, "Review baru");
  assert.equal(worker.shown[0].options.body, "2 transaksi menunggu tinjauan.");
  assert.equal(worker.shown[0].options.data.url, "/inbox");
});

test("a push with no body still shows a notification", async () => {
  const worker = createWorker();
  await worker.dispatch("push", { data: null });
  assert.equal(worker.shown.length, 1);
  assert.equal(worker.shown[0].title, "Richmod");
  assert.match(worker.shown[0].options.body, /kotak masuk/);
  assert.equal(worker.shown[0].options.data.url, "/inbox");
});

test("a push with malformed JSON still shows a notification", async () => {
  const worker = createWorker();
  await worker.dispatch("push", { data: { json: async () => { throw new SyntaxError("Unexpected token <"); } } });
  assert.equal(worker.shown.length, 1);
  assert.equal(worker.shown[0].title, "Richmod");
  assert.ok(worker.shown[0].options.body.length > 0);
});

test("a push with JSON that is not an object still shows a notification", async () => {
  const worker = createWorker();
  await worker.dispatch("push", { data: { json: async () => "not-a-payload" } });
  assert.equal(worker.shown.length, 1);
  assert.equal(worker.shown[0].title, "Richmod");
});

test("notificationclick focuses an open window and navigates it to the payload target", async () => {
  const focused = [];
  const navigated = [];
  const worker = createWorker({ windows: [{ url: `${origin}/transactions`, focus: async () => focused.push(true), navigate: async url => navigated.push(url) }] });
  const event = click("/settings");
  await worker.dispatch("notificationclick", event);
  assert.equal(event.notification.closed, true, "the notification is closed first");
  assert.deepEqual(focused, [true]);
  assert.deepEqual(navigated, ["/settings"]);
  assert.deepEqual(worker.opened, [], "no second window is opened");
});

test("notificationclick opens a window when none is open", async () => {
  const worker = createWorker({ windows: [] });
  await worker.dispatch("notificationclick", click("/settings"));
  assert.deepEqual(worker.opened, ["/settings"]);
});

test("notificationclick refuses an off-origin target and falls back to the inbox", async () => {
  const worker = createWorker({ windows: [] });
  await worker.dispatch("notificationclick", click("https://elsewhere.example/phish"));
  assert.deepEqual(worker.opened, ["/inbox"]);
});

test("notificationclick treats a protocol-relative target as off-origin", async () => {
  const worker = createWorker({ windows: [] });
  await worker.dispatch("notificationclick", click("//elsewhere.example/phish"));
  assert.deepEqual(worker.opened, ["/inbox"]);
});

test("a same-origin absolute target is reduced to its path", async () => {
  const worker = createWorker({ windows: [] });
  await worker.dispatch("notificationclick", click(`${origin}/inbox?view=actions`));
  assert.deepEqual(worker.opened, ["/inbox?view=actions"]);
});

test("notificationclick without a target opens the inbox", async () => {
  const worker = createWorker({ windows: [] });
  await worker.dispatch("notificationclick", click(undefined));
  assert.deepEqual(worker.opened, ["/inbox"]);
});
