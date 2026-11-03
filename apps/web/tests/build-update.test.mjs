// The update notice has no service worker state to lean on: the worker
// skipWaiting()s and keeps no cache, so `registration.waiting` is always null.
// What is checked here is the comparison itself: the id baked into the page
// against the id the server serves, with the polling rules around it.
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { deployedIdFrom, fetchDeployedId, startBuildWatch } from "../app/lib/buildWatch.js";
import { GENERATED_MODULE, SERVED_FILE, computeBuildId, writeBuildId } from "../scripts/build-id.mjs";

const read = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
const settle = () => new Promise(resolve => setImmediate(resolve));

function harness({ ownId = "aaaaaaaaaaaa", answer, visibility = "visible" } = {}) {
  const timers = [];
  const listeners = new Set();
  const doc = {
    visibilityState: visibility,
    addEventListener: (type, listener) => { if (type === "visibilitychange") listeners.add(listener); },
    removeEventListener: (type, listener) => { if (type === "visibilitychange") listeners.delete(listener); },
  };
  const updates = [];
  let fetches = 0;
  const watch = startBuildWatch({
    ownId,
    fetchDeployedId: async () => {
      fetches += 1;
      return answer();
    },
    onUpdate: () => updates.push(1),
    document: doc,
    interval: 1000,
    setTimer: fn => { timers.push(fn); return fn; },
    clearTimer: fn => { timers.splice(timers.indexOf(fn), 1); },
  });
  const emit = state => {
    doc.visibilityState = state;
    for (const listener of listeners) listener();
  };
  return {
    watch,
    updates,
    fetches: () => fetches,
    scheduled: () => timers.length,
    emit,
    async poll() {
      for (const timer of timers.splice(0)) timer();
      await settle();
    },
  };
}

test("the same id on both sides stays quiet; a different id asks for a reload", async () => {
  const same = harness({ answer: async () => "aaaaaaaaaaaa" });
  await same.poll();
  assert.equal(same.fetches(), 1, "the poll happened");
  assert.deepEqual(same.updates, [], "no banner for the id this page already runs");

  const newer = harness({ answer: async () => "bbbbbbbbbbbb" });
  await newer.poll();
  assert.deepEqual(newer.updates, [1], "a deployed id that differs offers the reload");
});

test("a failed fetch is not an update and does not throw", async () => {
  const watcher = harness({ answer: async () => { throw new Error("offline"); } });
  await watcher.poll();
  assert.equal(watcher.fetches(), 1);
  assert.deepEqual(watcher.updates, []);
});

test("only a readable id body counts: non-200, empty body and HTML are not updates", async () => {
  const response = (ok, body) => async () => ({ ok, text: async () => body });
  assert.equal(await fetchDeployedId(response(false, "bbbbbbbbbbbb")), null, "non-200");
  assert.equal(await fetchDeployedId(response(true, "")), null, "empty body");
  assert.equal(await fetchDeployedId(response(true, "  ")), null, "whitespace body");
  assert.equal(await fetchDeployedId(response(true, "<html>login</html>")), null, "captive portal page");
  assert.equal(await fetchDeployedId(response(true, "bbbbbbbbbbbb\n")), "bbbbbbbbbbbb", "the served id, with its newline");
  assert.equal(deployedIdFrom("not a hash!"), null);
});

test("a hidden tab does not poll and becoming visible checks exactly once", async () => {
  const watcher = harness({ answer: async () => "bbbbbbbbbbbb" });
  assert.equal(watcher.scheduled(), 1, "one poll is scheduled while visible");

  watcher.emit("hidden");
  assert.equal(watcher.scheduled(), 0, "the timer is cleared when the tab is hidden");
  await watcher.poll();
  assert.equal(watcher.fetches(), 0, "a hidden tab makes no request");

  watcher.emit("visible");
  await settle();
  assert.equal(watcher.fetches(), 1, "returning to the tab checks once");
  assert.deepEqual(watcher.updates, [1]);
});

test("dismissal suppresses the notice for the rest of this page load", async () => {
  const watcher = harness({ answer: async () => "bbbbbbbbbbbb" });
  watcher.watch.dismiss();
  assert.equal(watcher.scheduled(), 0, "dismissal stops the polling too");
  await watcher.poll();
  watcher.emit("visible");
  await settle();
  assert.deepEqual(watcher.updates, [], "no later release re-opens a dismissed notice this load");
});

test("the id baked into the page and the id served for the deployed build come from one hash", () => {
  const before = computeBuildId();
  const written = writeBuildId();
  assert.equal(written, before, "hashing is stable, and does not include its own output");
  assert.equal(computeBuildId(), before);
  assert.match(read(GENERATED_MODULE), new RegExp(`export const BUILD_ID = "${written}";`), "the page can read its own id with no request");
  assert.equal(read(SERVED_FILE), `${written}\n`, "the server serves the same bytes");
});
