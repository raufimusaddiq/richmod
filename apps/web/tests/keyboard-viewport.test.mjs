import assert from "node:assert/strict";
import test from "node:test";
import { attachKeyboardViewport, isKeyboardField, measureVisualViewport } from "../app/lib/keyboardViewport.js";

function createEmitter() {
  const listeners = new Map();
  return {
    addEventListener(type, fn) {
      if (!listeners.has(type)) listeners.set(type, new Set());
      listeners.get(type).add(fn);
    },
    removeEventListener(type, fn) {
      listeners.get(type)?.delete(fn);
    },
    dispatch(type, event = {}) {
      for (const fn of [...(listeners.get(type) || [])]) fn(event);
    },
    count(type) {
      return listeners.get(type)?.size || 0;
    },
  };
}

function createFakeEnv({ innerHeight = 800, visualViewport = { height: 800, offsetTop: 0, scale: 1 } } = {}) {
  const props = new Map();
  const classes = new Set();
  const root = {
    style: {
      setProperty(name, value) { props.set(name, value); },
      removeProperty(name) { props.delete(name); },
      getPropertyValue(name) { return props.get(name) || ""; },
    },
    classList: {
      toggle(name, force) {
        if (force === undefined) {
          if (classes.has(name)) classes.delete(name);
          else classes.add(name);
        } else if (force) classes.add(name);
        else classes.delete(name);
      },
      remove(name) { classes.delete(name); },
      contains(name) { return classes.has(name); },
    },
  };
  const docEmitter = createEmitter();
  const doc = {
    documentElement: root,
    activeElement: null,
    addEventListener: docEmitter.addEventListener,
    removeEventListener: docEmitter.removeEventListener,
    dispatch: docEmitter.dispatch,
    listenerCount: docEmitter.count,
  };
  const vvEmitter = createEmitter();
  const vv = visualViewport ? {
    height: visualViewport.height,
    offsetTop: visualViewport.offsetTop ?? 0,
    scale: visualViewport.scale ?? 1,
    addEventListener: vvEmitter.addEventListener,
    removeEventListener: vvEmitter.removeEventListener,
    dispatch: vvEmitter.dispatch,
    listenerCount: vvEmitter.count,
  } : null;
  const winEmitter = createEmitter();
  const win = {
    innerHeight,
    visualViewport: vv,
    addEventListener: winEmitter.addEventListener,
    removeEventListener: winEmitter.removeEventListener,
    dispatch: winEmitter.dispatch,
    listenerCount: winEmitter.count,
  };
  return { win, doc, vv, root };
}

test("isKeyboardField identifies editable inputs, selects, and textareas while skipping non-text controls", () => {
  assert.equal(isKeyboardField({ tagName: "INPUT", type: "text" }), true);
  assert.equal(isKeyboardField({ tagName: "INPUT", type: "search" }), true);
  assert.equal(isKeyboardField({ tagName: "INPUT", type: "email" }), true);
  assert.equal(isKeyboardField({ tagName: "INPUT", type: "password" }), true);
  assert.equal(isKeyboardField({ tagName: "TEXTAREA" }), true);
  assert.equal(isKeyboardField({ tagName: "SELECT" }), true);
  assert.equal(isKeyboardField({ tagName: "INPUT", type: "checkbox" }), false);
  assert.equal(isKeyboardField({ tagName: "INPUT", type: "radio" }), false);
  assert.equal(isKeyboardField({ tagName: "INPUT", type: "file" }), false);
  assert.equal(isKeyboardField({ tagName: "INPUT", type: "text", readOnly: true }), false);
  assert.equal(isKeyboardField({ tagName: "INPUT", type: "text", disabled: true }), false);
  assert.equal(isKeyboardField({ tagName: "BUTTON" }), false);
  assert.equal(isKeyboardField(null), false);
});

test("measureVisualViewport computes keyboard inset and height across normal, shrunk, scrolled, zoomed, and fallback states", () => {
  assert.deepEqual(measureVisualViewport({ innerHeight: 800, visualViewport: { height: 800, offsetTop: 0, scale: 1 } }), { inset: 0, height: 800 });
  assert.deepEqual(measureVisualViewport({ innerHeight: 800, visualViewport: { height: 460, offsetTop: 0, scale: 1 } }), { inset: 340, height: 460 });
  assert.deepEqual(measureVisualViewport({ innerHeight: 800, visualViewport: { height: 460, offsetTop: 40, scale: 1 } }), { inset: 300, height: 460 });
  assert.deepEqual(measureVisualViewport({ innerHeight: 800, visualViewport: { height: 400, offsetTop: 0, scale: 2 } }), { inset: 0, height: 400 });
  assert.deepEqual(measureVisualViewport({ innerHeight: 740, visualViewport: null }), { inset: 0, height: 740 });
});

test("attachKeyboardViewport writes --kb-inset and --vv-height, toggles classes, centers focused field on focus and resize, and cleans up", () => {
  const { win, doc, vv, root } = createFakeEnv({ innerHeight: 800, visualViewport: { height: 800, offsetTop: 0, scale: 1 } });
  const scrolled = [];
  const amount = { tagName: "INPUT", type: "text", scrollIntoView(options) { scrolled.push(["amount", options]); } };
  const checkbox = { tagName: "INPUT", type: "checkbox", scrollIntoView(options) { scrolled.push(["checkbox", options]); } };

  const detach = attachKeyboardViewport(win, doc);
  assert.equal(root.style.getPropertyValue("--kb-inset"), "0px");
  assert.equal(root.style.getPropertyValue("--vv-height"), "800px");
  assert.equal(root.classList.contains("kb-open"), false);
  assert.equal(root.classList.contains("kb-focused"), false);

  doc.activeElement = amount;
  doc.dispatch("focusin", { target: amount });
  assert.equal(root.classList.contains("kb-focused"), true);
  assert.deepEqual(scrolled, [["amount", { block: "center" }]]);

  vv.height = 470;
  vv.dispatch("resize");
  assert.equal(root.style.getPropertyValue("--kb-inset"), "330px");
  assert.equal(root.style.getPropertyValue("--vv-height"), "470px");
  assert.equal(root.classList.contains("kb-open"), true);
  assert.equal(scrolled.length, 2);
  assert.deepEqual(scrolled[1], ["amount", { block: "center" }]);

  vv.offsetTop = 20;
  vv.dispatch("scroll");
  assert.equal(root.style.getPropertyValue("--kb-inset"), "310px");
  assert.equal(scrolled.length, 2);

  doc.activeElement = checkbox;
  doc.dispatch("focusout", { target: amount, relatedTarget: checkbox });
  doc.dispatch("focusin", { target: checkbox });
  assert.equal(root.classList.contains("kb-focused"), false);
  assert.equal(scrolled.length, 2);

  vv.height = 800;
  vv.offsetTop = 0;
  vv.dispatch("resize");
  assert.equal(root.style.getPropertyValue("--kb-inset"), "0px");
  assert.equal(root.style.getPropertyValue("--vv-height"), "800px");
  assert.equal(root.classList.contains("kb-open"), false);

  detach();
  assert.equal(root.style.getPropertyValue("--kb-inset"), "");
  assert.equal(root.style.getPropertyValue("--vv-height"), "");
  assert.equal(root.classList.contains("kb-open"), false);
  assert.equal(root.classList.contains("kb-focused"), false);
  assert.equal(vv.listenerCount("resize"), 0);
  assert.equal(vv.listenerCount("scroll"), 0);
  assert.equal(win.listenerCount("resize"), 0);
  assert.equal(doc.listenerCount("focusin"), 0);
  assert.equal(doc.listenerCount("focusout"), 0);
});
