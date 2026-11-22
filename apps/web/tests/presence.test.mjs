import assert from "node:assert/strict";
import { mock, test } from "node:test";
import { EXIT_LIMIT_MS, initialPresence, nextPresence, whenSettled } from "../app/components/presenceState.js";

const flush = () => new Promise(resolve => setImmediate(resolve));

test("a hidden overlay leaves, then is gone only after it settles", () => {
  let state = initialPresence(true);
  assert.equal(state.phase, "open");
  state = nextPresence(state, "hide");
  assert.equal(state.phase, "leaving");
  assert.equal(nextPresence(state, "hide"), state, "hiding twice changes nothing");
  state = nextPresence(state, "settled");
  assert.equal(state.phase, "gone");
  assert.equal(nextPresence(state, "settled"), state);
});

test("showing again while leaving cancels the exit and remounts the tree", () => {
  const leaving = nextPresence(initialPresence(true), "hide");
  const reopened = nextPresence(leaving, "show");
  assert.equal(reopened.phase, "open");
  assert.equal(reopened.generation, leaving.generation + 1, "a new generation forces a fresh mount of the late request");
  assert.equal(nextPresence(reopened, "settled"), reopened, "a stale settle cannot unmount the reopened overlay");
  assert.equal(nextPresence(reopened, "show"), reopened);
});

test("showing after it is gone mounts without bumping the generation", () => {
  const gone = nextPresence(nextPresence(initialPresence(true), "hide"), "settled");
  const reopened = nextPresence(gone, "show");
  assert.deepEqual(reopened, { phase: "open", generation: gone.generation });
});

test("whenSettled finishes at once when nothing is animating", () => {
  mock.timers.enable({ apis: ["setTimeout"] });
  try {
    let done = 0;
    whenSettled({ getAnimations: () => [] }, () => { done += 1; });
    whenSettled({}, () => { done += 1; });
    assert.equal(done, 2);
  } finally { mock.timers.reset(); }
});

test("whenSettled waits for every animation to settle, rejected ones included", async () => {
  mock.timers.enable({ apis: ["setTimeout"] });
  try {
    let resolveFirst;
    let rejectSecond;
    const animations = [
      { finished: new Promise(resolve => { resolveFirst = resolve; }) },
      { finished: new Promise((_, reject) => { rejectSecond = reject; }) },
    ];
    let done = 0;
    whenSettled({ getAnimations: () => animations }, () => { done += 1; });
    resolveFirst();
    await flush();
    assert.equal(done, 0, "one running animation keeps the overlay mounted");
    rejectSecond(new Error("cancelled"));
    await flush();
    assert.equal(done, 1);
    mock.timers.tick(EXIT_LIMIT_MS * 2);
    assert.equal(done, 1, "the cap timer does not fire a second time");
  } finally { mock.timers.reset(); }
});

test("whenSettled gives up at the exit cap when an animation never finishes", () => {
  mock.timers.enable({ apis: ["setTimeout"] });
  try {
    let done = 0;
    whenSettled({ getAnimations: () => [{ finished: new Promise(() => {}) }] }, () => { done += 1; });
    mock.timers.tick(EXIT_LIMIT_MS - 1);
    assert.equal(done, 0);
    mock.timers.tick(1);
    assert.equal(done, 1);
  } finally { mock.timers.reset(); }
});

test("a cancelled whenSettled never calls back, even when animations finish later", async () => {
  mock.timers.enable({ apis: ["setTimeout"] });
  try {
    let resolve;
    let done = 0;
    const cancel = whenSettled({ getAnimations: () => [{ finished: new Promise(r => { resolve = r; }) }] }, () => { done += 1; });
    cancel();
    resolve();
    await flush();
    mock.timers.tick(EXIT_LIMIT_MS * 2);
    assert.equal(done, 0);
  } finally { mock.timers.reset(); }
});
