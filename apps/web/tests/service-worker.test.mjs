// Runs the real service worker template in a VM with a controllable Cache
// Storage, so the failure paths that cannot be reproduced against a browser are
// still executed: a denied cache open, a rejected put, and a worker upgrade
// while a page from the previous build is still running.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { runInNewContext } from "node:vm";
import test from "node:test";

const template = readFileSync(new URL("../scripts/service-worker.template.js", import.meta.url), "utf8");
const origin = "https://richmod.test";
const entryKey = request => new URL(typeof request === "string" ? request : request.url, origin).href;

function createStorage({ failOpen = false, failPut = false, network = request => new Response(`network:${request.url}`, { status: 200 }) } = {}) {
  const stores = new Map();
  const storage = {
    requests: [],
    network,
    async open(name) {
      if (failOpen) throw new Error("cache storage unavailable");
      if (!stores.has(name)) stores.set(name, new Map());
      const entries = stores.get(name);
      return {
        async match(request) {
          const hit = entries.get(entryKey(request));
          return hit ? hit.clone() : undefined;
        },
        async put(request, response) {
          if (failPut) throw new Error("quota exceeded");
          entries.set(entryKey(request), response.clone());
        },
      };
    },
    async keys() {
      return [...stores.keys()];
    },
    async delete(name) {
      return stores.delete(name);
    },
    async match(request) {
      const key = entryKey(request);
      for (const entries of stores.values()) {
        const hit = entries.get(key);
        if (hit) return hit.clone();
      }
      return undefined;
    },
    entries(name) {
      return stores.get(name);
    },
  };
  return storage;
}

function createWorker(build, storage, assets = []) {
  const listeners = {};
  const sandbox = {
    self: {
      location: { origin },
      addEventListener: (type, handler) => { listeners[type] = handler; },
      skipWaiting: () => {},
      clients: { claim: async () => {} },
    },
    caches: storage,
    fetch: input => {
      const request = typeof input === "string" ? new Request(new URL(input, origin)) : input;
      storage.requests.push(request.url);
      return Promise.resolve(storage.network(request));
    },
    Request,
    Response,
    URL,
  };
  const source = template.replaceAll("__BUILD_ID__", build).replace("__ASSETS__", JSON.stringify(assets));
  runInNewContext(source, sandbox);
  return {
    async install() {
      const event = { waitUntil(promise) { event.done = promise; } };
      listeners.install(event);
      await event.done;
    },
    async fetch(url) {
      const event = { request: new Request(url), respondWith(promise) { event.response = promise; } };
      listeners.fetch(event);
      assert.ok(event.response, "worker intercepted the static chunk");
      return event.response;
    },
    async activate() {
      const event = { waitUntil(promise) { event.done = promise; } };
      listeners.activate(event);
      await event.done;
    },
  };
}

test("a denied cache open still returns the fetched chunk", async () => {
  const storage = createStorage({ failOpen: true, network: () => new Response("chunk-bytes", { status: 200 }) });
  const worker = createWorker("build1", storage);
  const response = await worker.fetch(`${origin}/_next/static/chunks/a.js`);
  assert.equal(response.status, 200);
  assert.equal(await response.text(), "chunk-bytes");
  assert.equal(storage.requests.length, 1, "the network was asked exactly once");
});

test("a rejected cache put still returns the fetched chunk", async () => {
  const storage = createStorage({ failPut: true, network: () => new Response("chunk-bytes", { status: 200 }) });
  const worker = createWorker("build1", storage);
  const response = await worker.fetch(`${origin}/_next/static/chunks/a.js`);
  assert.equal(response.status, 200);
  assert.equal(await response.text(), "chunk-bytes");
  assert.equal(storage.entries("richmod-static-build1").size, 0, "nothing was cached");
});

test("install caches the complete chunk set, not just visited chunks", async () => {
  const first = `${origin}/_next/static/chunks/a.js`;
  const second = `${origin}/_next/static/chunks/b.js`;
  const storage = createStorage();
  const worker = createWorker("build1", storage, [first, second]);
  await worker.install();
  const entries = storage.entries("richmod-static-build1");
  assert.ok(entries.has(first) && entries.has(second), "every exported chunk is precached");
});

test("an open client gets a chunk it never fetched before the deploy", async () => {
  const oldChunk = `${origin}/_next/static/chunks/old.js`;
  const shared = `${origin}/_next/static/chunks/shared.js`;
  let deployed = false;
  const storage = createStorage({
    network: request => {
      const path = new URL(request.url).pathname;
      if (deployed && path === "/_next/static/chunks/old.js") return new Response("not found", { status: 404 });
      return new Response(`bytes:${path}`, { status: 200 });
    },
  });

  // Build A installs and precaches its whole set; the client never asks for
  // old.js, so only the install-time precache can have it.
  const first = createWorker("buildA", storage, [oldChunk, shared]);
  await first.install();
  assert.ok(storage.entries("richmod-static-buildA").has(oldChunk), "install precached the unvisited chunk");

  // Deploy build B: the server no longer has A's assets.
  deployed = true;
  const second = createWorker("buildB", storage, [shared]);
  await second.install();
  await second.activate();
  assert.ok(storage.entries("richmod-static-buildA"), "the previous generation was retained");

  const networkCallsBefore = storage.requests.length;
  const stillOpen = await second.fetch(oldChunk);
  assert.equal(stillOpen.status, 200, "the open client's unvisited chunk still resolves");
  assert.equal(await stillOpen.text(), "bytes:/_next/static/chunks/old.js");
  assert.equal(storage.requests.length, networkCallsBefore, "no request went to the deleted asset");

  // The next release prunes only the generation before the previous one.
  const third = createWorker("buildC", storage, [shared]);
  await third.install();
  await third.activate();
  assert.equal(storage.entries("richmod-static-buildA"), undefined, "two-generations-old cache is pruned");
  assert.ok(storage.entries("richmod-static-buildB"), "the immediately previous generation survives");
  const generations = await (await storage.open("richmod-generations")).match("/generations").then(response => response.json());
  assert.deepEqual(generations, ["buildB", "buildC"], "the ordered generation list keeps previous and current only");
});
