// Runs the real pass-through worker in a VM. The point of this file is that
// there is no caching path left to test: a chunk request is never intercepted.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import test from "node:test";

const source = readFileSync(new URL("../public/sw.js", import.meta.url), "utf8");

function createWorker({ keys = [] } = {}) {
  const listeners = {};
  const deleted = [];
  let claimed = false;
  const sandbox = {
    self: {
      addEventListener: (type, handler) => { listeners[type] = handler; },
      skipWaiting: () => {},
      clients: { claim: async () => { claimed = true; } },
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
  return {
    listeners,
    deleted,
    isClaimed: () => claimed,
    async activate() {
      const event = { waitUntil(promise) { event.done = promise; } };
      listeners.activate(event);
      await event.done;
    },
  };
}

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

test("activation deletes only the abandoned cache stores", async () => {
  const worker = createWorker({ keys: ["richmod-static-abc123", "richmod-generations", "unrelated"] });
  await worker.activate();
  assert.deepEqual(worker.deleted, ["richmod-static-abc123", "richmod-generations"]);
  assert.equal(worker.isClaimed(), true, "the pass-through worker still takes control");
});
