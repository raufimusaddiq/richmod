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

function createMessageChannel() {
  const channel = {};
  channel.port1 = { onmessage: null, postMessage: data => queueMicrotask(() => channel.port2.onmessage?.({ data })) };
  channel.port2 = { onmessage: null, postMessage: data => queueMicrotask(() => channel.port1.onmessage?.({ data })) };
  return channel;
}

function windowClient(chunk) {
  return {
    type: "window",
    postMessage(message, transfer) {
      if (message?.type === "richmod:generation" && transfer?.[0]) transfer[0].postMessage([chunk]);
    },
  };
}

function createStorage({ failOpen = false, failPut = false, network = request => new Response(`network:${request.url}`, { status: 200 }) } = {}) {
  const stores = new Map();
  const storage = {
    requests: [],
    network,
    windows: [],
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
    async match(request, options = {}) {
      const key = entryKey(request);
      const names = options.cacheName ? [options.cacheName] : [...stores.keys()];
      for (const name of names) {
        const hit = stores.get(name)?.get(key);
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
      clients: { claim: async () => {}, matchAll: async () => storage.windows },
    },
    caches: storage,
    MessageChannel: createMessageChannel,
    setTimeout,
    clearTimeout,
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

test("an A client held open across B and C still gets its unvisited chunk", async () => {
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

  // Build A installs and activate; the tab then stays open for the whole test.
  // It never asks for old.js, so only the install-time precache can have it.
  const first = createWorker("buildA", storage, [oldChunk, shared]);
  await first.install();
  await first.activate();
  assert.ok(storage.entries("richmod-static-buildA").has(oldChunk), "install precached the unvisited chunk");
  storage.windows = [windowClient(oldChunk)];

  // Deploy build B: the server no longer has A's assets.
  deployed = true;
  const second = createWorker("buildB", storage, [shared]);
  await second.install();
  await second.activate();

  // Deploy build C while the A tab is still open. A plain two-generation window
  // would slide to B and C and delete A here.
  const third = createWorker("buildC", storage, [shared]);
  await third.install();
  await third.activate();
  assert.ok(storage.entries("richmod-static-buildA"), "the generation an open window claims survives two activations");

  const networkCallsBefore = storage.requests.length;
  const stillOpen = await third.fetch(oldChunk);
  assert.equal(stillOpen.status, 200, "the open client's unvisited chunk still resolves");
  assert.equal(await stillOpen.text(), "bytes:/_next/static/chunks/old.js");
  assert.equal(storage.requests.length, networkCallsBefore, "no request went to the deleted asset");
  const generations = await (await storage.open("richmod-generations")).match("/generations").then(response => response.json());
  assert.deepEqual(generations, ["buildA", "buildB", "buildC"], "all three generations are recorded while A is claimed");
});

test("pruning stays bounded when many builds remain claimed", async () => {
  const storage = createStorage();
  for (let index = 0; index < 6; index += 1) {
    const chunk = `${origin}/_next/static/chunks/chunk-${index}.js`;
    const worker = createWorker(`build${index}`, storage, [chunk]);
    await worker.install();
    await worker.activate();
    storage.windows.push(windowClient(chunk));
  }
  const statics = (await storage.keys()).filter(name => name.startsWith("richmod-static-"));
  assert.equal(statics.length, 4, "the cache window never exceeds MAX_GENERATIONS");
  assert.equal(storage.entries("richmod-static-build0"), undefined, "the oldest claimed generation is dropped at the cap");
  assert.ok(storage.entries("richmod-static-build5"), "the current generation is always kept");
});
