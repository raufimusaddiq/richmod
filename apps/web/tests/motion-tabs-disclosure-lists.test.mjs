import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { globalCss } from "./source.mjs";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");
const motionCss = () => text("app/styles/15-motion.css");

test("the observer is mounted once from the app root", () => {
  assert.match(text("app/App.js"), /import MotionEffects from "\.\/components\/MotionEffects";/);
  assert.equal((text("app/App.js").match(/<MotionEffects \/>/g) || []).length, 1);
  assert.match(text("app/components/MotionEffects.js"), /attachMotion/);
});

test("every tab strip and segmented control opts into the sliding indicator", () => {
  const sites = [
    ["app/analytics/page.js", /className="report-switch" data-slide data-slide-panel="#laporan-panel" role="tablist"/],
    ["app/analytics/page.js", /className="range-control-group" data-slide data-slide-panel="[^"]+" role="group" aria-label="Tampilan analisis"/],
    ["app/analytics/CalendarReview.js", /className="range-control-group" data-slide data-slide-panel="\.calendar-body"/],
    ["app/analytics/CycleSections.js", /className="segmented" data-slide role="group"/],
    ["app/components/QuickRecordDialog.js", /className="segmented" data-slide role="group" aria-label="Jenis transaksi"/],
    ["app/documents/page.js", /className="filter-chips" data-slide data-slide-panel="\.document-grid"/],
    ["app/household/page.js", /className="settings-tabs" data-slide data-slide-panel="#settings-panel" role="tablist"/],
    ["app/admin/page.js", /className="admin-tabs" data-slide data-slide-panel="#admin-panel" role="tablist"/],
    ["app/inbox/page.js", /className="inbox-tabs" data-slide data-slide-panel="#inbox-panel" role="tablist"/],
  ];
  for (const [file, pattern] of sites) assert.match(text(file), pattern, `${file} lost its slide marker`);
});

test("first-load lists opt into the stagger", () => {
  assert.match(text("app/components/TransactionList.js"), /data-stagger="\.transaction-row"/);
  assert.match(text("app/documents/page.js"), /data-stagger="\.document-card"/);
  assert.match(text("app/components/CategoryBars.js"), /data-stagger=":scope > li"/);
  assert.match(text("app/household/page.js"), /data-stagger=":scope > article"/);
  for (const file of ["LLM", "Overview", "Reviews"]) assert.match(text(`app/admin/${file}.js`), /className="admin-metrics" data-stagger="\.admin-metric"/);
});

test("the mechanism keeps no timers and never writes the attributes it observes", () => {
  const source = text("app/lib/interactionMotion.js");
  assert.doesNotMatch(source, /setTimeout|setInterval/, "timing belongs to CSS");
  const observed = source.match(/const WATCHED = \[([^\]]+)\]/)[1];
  assert.match(observed, /"class"/);
  assert.match(observed, /"aria-selected"/);
  assert.match(observed, /"aria-pressed"/);
  assert.doesNotMatch(observed, /data-|style/, "the observer must not watch its own writes");
  assert.match(source, /"\[aria-selected=\\"true\\"\], \[aria-pressed=\\"true\\"\], \.active"|:is\(\[aria-selected="true"\], \[aria-pressed="true"\], \.active\)/);
});

test("the stagger runs once per container and releases on the animations' own end", () => {
  const source = text("app/lib/interactionMotion.js");
  assert.match(source, /if \(started\.has\(container\)\) return false;/);
  assert.match(source, /animation\.finished/);
  assert.match(source, /removeAttribute\("data-stagger-item"\)/);
});

test("motion tokens and the stagger cap are declared once", () => {
  const all = globalCss();
  assert.match(all, /--motion-slide: 300ms;/);
  assert.match(all, /--motion-stagger: 45ms;/);
  assert.match(all, /--ease-glide: /);
  const motion = motionCss();
  const guard = motion.indexOf("@media (prefers-reduced-motion: reduce)");
  const added = motion.slice(motion.indexOf("[data-slide] { position: relative;"));
  assert.ok(guard >= 0 && motion.indexOf("[data-slide] { position: relative;") > guard, "the new block follows the guard");
  assert.doesNotMatch(added, /\b\d*\.?\d+m?s\b(?!\))/, "the new rules read tokens, not literal durations");
  assert.match(added, /min\(var\(--stagger-i, 0\), 7\) \* var\(--motion-stagger\)/);
  assert.equal((globalCss().match(/prefers-reduced-motion: reduce/g) || []).length, 1);
});

test("animated rules are off for reduced motion and structural rules are not", () => {
  const added = motionCss().slice(motionCss().indexOf("[data-slide] { position: relative;"));
  const gated = added.slice(added.indexOf("@media (prefers-reduced-motion: no-preference)"));
  for (const rule of ["[data-stagger-item]", "[data-panel-enter]", "details::details-content", ".filter-more", "[data-slide][data-slide-ready]::before { transition"]) {
    assert.ok(gated.includes(rule), `${rule} must live under no-preference`);
    assert.ok(!added.slice(0, added.indexOf("@media (prefers-reduced-motion: no-preference)")).includes(rule), `${rule} must not be declared outside the gate`);
  }
});

test("a stagger never compounds with the route reveal", () => {
  assert.ok(motionCss().includes("[data-reveal] [data-stagger-item], [data-stagger-item][data-reveal] { animation: none; }"));
});

test("disclosures animate only where the browser supports it", () => {
  const added = motionCss();
  assert.match(added, /@supports \(interpolate-size: allow-keywords\) and selector\(details::details-content\)/);
  assert.match(added, /details\[open\]::details-content \{ block-size: auto; opacity: 1; \}/);
  assert.match(added, /content-visibility var\(--motion-slide\) allow-discrete/);
  assert.match(added, /\.filter-more\[hidden\] \{ height: 0;/);
});

test("the sliding pill replaces the active button's own paint with the same box", () => {
  const added = motionCss();
  assert.match(added, /\[data-slide\]::before \{ box-sizing: border-box;/, "pseudo-elements do not inherit the universal border-box rule");
  assert.match(added, /\[data-slide\]\[data-slide-ready\] > button:is\(\.active, \[aria-selected="true"\], \[aria-pressed="true"\]\) \{ border-color: transparent; background: transparent; box-shadow: none; \}/);
  for (const family of [".inbox-tabs, .admin-tabs, .range-control-group", ".report-switch", ".settings-tabs", ".filter-chips", ".segmented", ".quick-body .segmented"]) assert.ok(added.includes(`${family} { --slide-`), `${family} has no pill style`);
});

test("tab semantics stay untouched by the shared mechanism", () => {
  const source = text("app/lib/interactionMotion.js");
  assert.doesNotMatch(source, /setAttribute\("(role|aria-|tabindex)/i);
  assert.doesNotMatch(source, /\.focus\(|addEventListener\("key/);
});
