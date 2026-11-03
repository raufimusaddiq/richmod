// The update notice has no service worker state to lean on: the worker
// skipWaiting()s and keeps no cache, so `registration.waiting` is always null.
// What is checked here is the comparison itself: the id baked into the page
// against the id the server serves, with the polling rules around it.
import assert from "node:assert/strict";
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { deployedIdFrom, fetchDeployedId, startBuildWatch } from "../app/lib/buildWatch.js";
import { GENERATED_MODULE, SERVED_FILE, computeBuildId, hashedFiles, writeBuildId } from "../scripts/build-id.mjs";

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
  await settle();
  await same.poll();
  assert.equal(same.fetches(), 2, "the mount check and one poll happened");
  assert.deepEqual(same.updates, [], "no banner for the id this page already runs");

  const newer = harness({ answer: async () => "bbbbbbbbbbbb" });
  await settle();
  assert.deepEqual(newer.updates, [1], "a deployed id that differs offers the reload");
});

test("mounting checks at once, because every route remounts the shell", async () => {
  const watcher = harness({ answer: async () => "aaaaaaaaaaaa" });
  await settle();
  assert.equal(watcher.fetches(), 1, "the check runs without waiting for the interval");
  assert.equal(watcher.scheduled(), 1, "the interval still runs afterwards");

  // A navigation mounts a second watcher; it must check again immediately
  // instead of restarting a ten-minute timer the user keeps resetting.
  const next = harness({ answer: async () => "aaaaaaaaaaaa" });
  await settle();
  assert.equal(next.fetches(), 1, "the next navigation checks immediately too");
});

test("a hidden document checks nothing at mount", async () => {
  const watcher = harness({ answer: async () => "bbbbbbbbbbbb", visibility: "hidden" });
  await settle();
  assert.equal(watcher.fetches(), 0, "no request while hidden");
  assert.equal(watcher.scheduled(), 0, "and no timer armed");
  assert.deepEqual(watcher.updates, []);
});

test("a failed fetch is not an update and does not throw", async () => {
  const watcher = harness({ answer: async () => { throw new Error("offline"); } });
  await settle();
  await watcher.poll();
  assert.equal(watcher.fetches(), 2, "the mount check and one poll ran");
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
  await settle();
  assert.equal(watcher.scheduled(), 1, "one poll is scheduled while visible");
  const afterMount = watcher.fetches();
  const updatesAfterMount = watcher.updates.length;

  watcher.emit("hidden");
  assert.equal(watcher.scheduled(), 0, "the timer is cleared when the tab is hidden");
  await watcher.poll();
  assert.equal(watcher.fetches(), afterMount, "a hidden tab makes no request");

  watcher.emit("visible");
  await settle();
  assert.equal(watcher.fetches(), afterMount + 1, "returning to the tab checks once");
  assert.equal(watcher.updates.length, updatesAfterMount + 1, "and reports it once");
});

test("dismissal suppresses the notice for the rest of this page load", async () => {
  const watcher = harness({ answer: async () => "bbbbbbbbbbbb" });
  await settle();
  watcher.watch.dismiss();
  const seen = watcher.updates.length;
  assert.equal(watcher.scheduled(), 0, "dismissal stops the polling too");
  await watcher.poll();
  watcher.emit("visible");
  await settle();
  assert.equal(watcher.updates.length, seen, "no later release re-opens a dismissed notice this load");
});

test("the hashed set covers everything that feeds the export", () => {
  const files = hashedFiles();
  assert.ok(files.includes("scripts/splash-devices.mjs"), "app/layout.js imports it into the export");
  assert.ok(files.includes("app/layout.js") && files.includes("next.config.mjs"), "the app and build config are inputs");
  assert.ok(files.every(path => path !== SERVED_FILE && path !== GENERATED_MODULE), "only generated files are excluded");
  assert.ok(files.every((path, index) => index === 0 || files[index - 1] < path), "the set is sorted, so the hash is machine-independent");
});

test("editing a hashed input changes the id; the generated files do not", async () => {
  const dir = mkdtempSync(join(tmpdir(), "richmod-build-id-"));
  try {
    mkdirSync(join(dir, "app/lib"), { recursive: true });
    mkdirSync(join(dir, "public"), { recursive: true });
    mkdirSync(join(dir, "scripts"), { recursive: true });
    writeFileSync(join(dir, "app/layout.js"), "import x from '../scripts/splash-devices.mjs';\n");
    writeFileSync(join(dir, "public/manifest.webmanifest"), "{}\n");
    writeFileSync(join(dir, "scripts/splash-devices.mjs"), "export const splashDevices = [];\n");
    writeFileSync(join(dir, "next.config.mjs"), "export default {}\n");

    const before = computeBuildId(dir);
    assert.equal(computeBuildId(dir), before, "the same tree hashes the same");

    // A generated id file must not feed back into the hash, or it could never
    // stabilise; writing one changes nothing.
    writeFileSync(join(dir, GENERATED_MODULE), "export const BUILD_ID = 'x';\n");
    writeFileSync(join(dir, SERVED_FILE), "x\n");
    assert.equal(computeBuildId(dir), before, "generated output is excluded");

    // But a file the export imports is an input: touching it changes the id.
    writeFileSync(join(dir, "scripts/splash-devices.mjs"), "export const splashDevices = [{ file: 'a.png' }];\n");
    assert.notEqual(computeBuildId(dir), before, "an imported script is part of the build identity");

    // And a rename is a change too: the path is hashed with the contents.
    const renamed = computeBuildId(dir);
    writeFileSync(join(dir, "scripts/splash-devices.mjs"), "export const splashDevices = [{ file: 'a.png' }];\n");
    writeFileSync(join(dir, "scripts/devices.mjs"), "export const splashDevices = [{ file: 'a.png' }];\n");
    assert.notEqual(computeBuildId(dir), renamed, "the path is part of the identity");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});

test("the id baked into the page and the id served for the deployed build come from one hash", () => {
  const before = computeBuildId();
  const written = writeBuildId();
  assert.equal(written, before, "hashing is stable, and does not include its own output");
  assert.equal(computeBuildId(), before);
  assert.match(read(GENERATED_MODULE), new RegExp(`export const BUILD_ID = "${written}";`), "the page can read its own id with no request");
  assert.equal(read(SERVED_FILE), `${written}\n`, "the server serves the same bytes");
});
