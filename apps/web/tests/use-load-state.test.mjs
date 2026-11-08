// Runtime test of the shared load helpers. There is no DOM in this suite, so a
// minimal hook dispatcher stands in for the renderer: state, refs, memoised
// callbacks and effects with cleanup, re-rendering synchronously on a change.
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import test from "node:test";
import swc from "next/dist/build/swc/index.js";

const require = createRequire(import.meta.url);
await swc.loadBindings();
const React = require("react");
const internals = React.__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE;

const file = path.resolve(new URL("../app/lib/useLoadState.js", import.meta.url).pathname);
const { code } = swc.transformSync(fs.readFileSync(file, "utf8"), { filename: file, jsc: { parser: { syntax: "ecmascript" } }, module: { type: "commonjs" } });
const mod = { exports: {} };
new Function("module", "exports", "require", code)(mod, mod.exports, require);
const { useGuardedLoad, useDrawerRecord, useBusyMap, LoadError, LOAD } = mod.exports;

function renderHook(callback) {
  const slots = [], effects = [];
  let index = 0, result, rendering = false, dirty = false;
  const same = (a, b) => a && b && a.length === b.length && a.every((value, position) => Object.is(value, b[position]));
  const dispatcher = {
    useState(initial) {
      const i = index++;
      if (!slots[i]) slots[i] = { value: typeof initial === "function" ? initial() : initial };
      const slot = slots[i];
      return [slot.value, next => { const value = typeof next === "function" ? next(slot.value) : next; if (!Object.is(value, slot.value)) { slot.value = value; render(); } }];
    },
    useRef(initial) { const i = index++; if (!slots[i]) slots[i] = { current: initial }; return slots[i]; },
    useCallback(fn, deps) { const i = index++; if (!slots[i] || !same(slots[i].deps, deps)) slots[i] = { deps, fn }; return slots[i].fn; },
    useMemo(fn, deps) { const i = index++; if (!slots[i] || !same(slots[i].deps, deps)) slots[i] = { deps, value: fn() }; return slots[i].value; },
    useEffect(fn, deps) { const i = index++; if (!effects[i] || !same(effects[i].deps, deps)) { effects[i] = { ...effects[i], pending: fn, deps }; } },
  };
  function render() {
    if (rendering) { dirty = true; return; }
    rendering = true;
    do {
      dirty = false; index = 0;
      const previous = internals.H; internals.H = dispatcher;
      try { result = callback(); } finally { internals.H = previous; }
      for (const effect of effects) if (effect?.pending) { effect.cleanup?.(); const run = effect.pending; effect.pending = null; effect.cleanup = run(); }
    } while (dirty);
    rendering = false;
  }
  render();
  return { get current() { return result; }, unmount() { for (const effect of effects) effect?.cleanup?.(); } };
}

const deferred = () => { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b; }); return { promise, resolve, reject }; };
const tick = () => new Promise(resolve => setImmediate(resolve));

test("useGuardedLoad: a superseded run is aborted and can neither set state nor fail the page", async () => {
  const gates = [], signals = [], applied = [];
  const hook = renderHook(() => useGuardedLoad(async ({ signal, current }, tag) => {
    signals.push(signal); const gate = deferred(); gates.push(gate); const value = await gate.promise;
    if (current()) applied.push(`${tag}:${value}`);
  }));
  hook.current.run("A"); hook.current.run("B");
  assert.equal(hook.current.status, LOAD.LOADING);
  assert.equal(signals[0].aborted, true, "starting B aborts A");
  gates[1].resolve("ok"); await tick();
  gates[0].resolve("late"); await tick();
  assert.deepEqual(applied, ["B:ok"], "the late response of A is ignored");
  assert.equal(hook.current.status, LOAD.READY);
  hook.unmount();
});

test("useGuardedLoad: a superseded failure is silent, a current one shows the fallback or a LoadError message", async () => {
  const gates = [];
  const hook = renderHook(() => useGuardedLoad(async () => { const gate = deferred(); gates.push(gate); await gate.promise; }, { fallback: "fallback" }));
  hook.current.run(); hook.current.run();
  gates[0].reject(new Error("old")); await tick();
  assert.equal(hook.current.status, LOAD.LOADING);
  assert.equal(hook.current.error, "");
  gates[1].reject(new TypeError("Failed to fetch")); await tick();
  assert.equal(hook.current.status, LOAD.ERROR);
  assert.equal(hook.current.error, "fallback", "browser jargon is not shown");
  hook.current.run(); gates[2].reject(new LoadError("pesan sendiri")); await tick();
  assert.equal(hook.current.error, "pesan sendiri");
  hook.unmount();
});

test("useGuardedLoad: after a first success a reload is `refreshing`, a first failure retries as `loading`", async () => {
  const gates = [];
  const hook = renderHook(() => useGuardedLoad(async () => { const gate = deferred(); gates.push(gate); await gate.promise; }));
  hook.current.run(); gates[0].reject(new Error("x")); await tick();
  assert.equal(hook.current.hasLoaded, false);
  hook.current.run();
  assert.equal(hook.current.status, LOAD.LOADING);
  gates[1].resolve(); await tick();
  assert.equal(hook.current.hasLoaded, true);
  hook.current.run();
  assert.equal(hook.current.status, LOAD.REFRESHING);
  gates[2].resolve(); await tick();
  assert.equal(hook.current.status, LOAD.READY);
  hook.unmount();
});

test("useBusyMap: a second start for the same key is refused and the key is released in finally", async () => {
  const hook = renderHook(() => useBusyMap());
  const gate = deferred();
  let calls = 0;
  const first = hook.current.run("a", async () => { calls += 1; await gate.promise; });
  const second = await hook.current.run("a", async () => { calls += 1; });
  assert.equal(second, undefined);
  assert.equal(calls, 1, "double submit never starts a second task");
  assert.equal(hook.current.busy.a, true);
  await hook.current.run("b", async () => {});
  assert.equal(hook.current.busy.a, true, "another key finishing does not release `a`");
  gate.resolve(); await first;
  assert.equal(hook.current.busy.a, undefined);
  await assert.rejects(hook.current.run("c", async () => { throw new Error("boom"); }), /boom/);
  assert.equal(hook.current.busy.c, undefined, "a throwing task still releases its key");
  assert.equal(await hook.current.run("c", async () => "again"), "again");
});

test("useDrawerRecord: a slow response never reopens a closed drawer or overwrites another record", async () => {
  const gates = {};
  const hook = renderHook(() => useDrawerRecord(async (id) => { gates[id] = deferred(); return gates[id].promise; }));
  hook.current.open("A"); hook.current.open("B");
  assert.equal(hook.current.id, "B");
  gates.B.resolve("data-B"); await tick();
  gates.A.resolve("data-A"); await tick();
  assert.equal(hook.current.status, "ready");
  assert.equal(hook.current.data, "data-B");
  hook.current.open("C"); hook.current.close();
  gates.C.resolve("data-C"); await tick();
  assert.equal(hook.current.status, "closed");
  assert.equal(hook.current.data, null);
  hook.current.open("D"); gates.D.reject(new Error("net")); await tick();
  assert.equal(hook.current.status, "error");
  hook.unmount();
});
