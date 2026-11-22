import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import { createElement } from "react";
import { renderToString } from "react-dom/server";
import { CHART_DRAW_MS, CHART_EASING, COUNT_UP_MS, chartMotionProps, countUpText, countUpValue, datasetKey, easeOutCubic, parseCountUpText, prefersReducedMotion, startCountUp, subscribeReducedMotion } from "../app/lib/motion.js";
import { usePrefersReducedMotion } from "../app/lib/useMotion.js";
import { money } from "../app/lib/format.js";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

function withMatchMedia(matches) {
  const listeners = new Set();
  const query = {
    matches,
    addEventListener: (_, callback) => listeners.add(callback),
    removeEventListener: (_, callback) => listeners.delete(callback),
  };
  const previous = globalThis.window;
  globalThis.window = { matchMedia: media => Object.assign(query, { media }) };
  return { query, listeners, restore: () => { if (previous === undefined) delete globalThis.window; else globalThis.window = previous; } };
}

test("reduced-motion preference follows prefers-reduced-motion and defaults to reduced without matchMedia", () => {
  const reduced = withMatchMedia(true);
  assert.equal(prefersReducedMotion(), true);
  assert.equal(reduced.query.media, "(prefers-reduced-motion: reduce)");
  reduced.restore();
  const full = withMatchMedia(false);
  assert.equal(prefersReducedMotion(), false);
  full.restore();
  assert.equal(prefersReducedMotion(), true, "no window means server render: treated as reduced");
});

test("subscribing to the reduced-motion preference registers and removes one change listener", () => {
  const env = withMatchMedia(false);
  const callback = () => {};
  const unsubscribe = subscribeReducedMotion(callback);
  assert.equal(env.listeners.size, 1);
  unsubscribe();
  assert.equal(env.listeners.size, 0);
  env.restore();
  assert.doesNotThrow(() => subscribeReducedMotion(callback)());
});

test("the reduced-motion hook renders reduced on the server so nothing animates before hydration", () => {
  function Probe() { return createElement("i", null, String(usePrefersReducedMotion())); }
  assert.match(renderToString(createElement(Probe)), /true/);
});

test("chart motion props animate over the chart duration only when active", () => {
  assert.deepEqual(chartMotionProps(true), { isAnimationActive: true, animationBegin: 0, animationDuration: CHART_DRAW_MS, animationEasing: CHART_EASING });
  const done = () => {};
  assert.equal(chartMotionProps(true, done).onAnimationEnd, done, "the series reports when its draw has finished");
  assert.equal(chartMotionProps(false).isAnimationActive, false);
  assert.ok(CHART_DRAW_MS >= 500 && CHART_DRAW_MS <= 800, "charts draw in between 500 and 800ms");
  assert.ok(COUNT_UP_MS >= 600 && COUNT_UP_MS <= 900, "counters run between 600 and 900ms");
});

test("the JS chart duration mirrors the CSS motion token", () => {
  assert.match(text("app/styles/01-tokens-and-base.css"), new RegExp(`--motion-chart: ${CHART_DRAW_MS}ms;`));
});

test("the dataset key is stable for equal data and changes with the data", () => {
  assert.equal(datasetKey([{ a: 1 }]), datasetKey([{ a: 1 }]));
  assert.notEqual(datasetKey([{ a: 1 }]), datasetKey([{ a: 2 }]));
  assert.equal(datasetKey(undefined), datasetKey(null));
});

test("easing starts at zero, ends at one and never overshoots", () => {
  assert.equal(easeOutCubic(0), 0);
  assert.equal(easeOutCubic(1), 1);
  assert.equal(easeOutCubic(2), 1);
  assert.equal(easeOutCubic(-1), 0);
  let previous = 0;
  for (let step = 1; step <= 20; step++) {
    const value = easeOutCubic(step / 20);
    assert.ok(value >= previous && value <= 1);
    previous = value;
  }
  assert.equal(countUpValue(100, 900, 1), 900);
  assert.equal(countUpValue(100, 900, 0), 100);
});

test("every formatted amount round-trips through the count-up text exactly", () => {
  for (const value of ["0", "7", "999", "1000", "6796525", "-6796525", "123456789012", "-1", "12345.67"]) {
    const formatted = money(value);
    const parsed = parseCountUpText(formatted);
    assert.ok(parsed, `${formatted} parses`);
    assert.equal(countUpText(parsed, parsed.value), formatted, `${formatted} rebuilds identically`);
  }
  const signed = parseCountUpText("+Rp 1.250.000");
  assert.equal(countUpText(signed, signed.value), "+Rp 1.250.000");
  assert.equal(parseCountUpText("—"), null, "text without digits is never animated");
  assert.equal(parseCountUpText("Rp 99999999999999999999"), null, "values beyond safe integers are never animated");
});

test("intermediate count-up frames keep the currency shape", () => {
  const parsed = parseCountUpText(money("-6796525"));
  assert.equal(parsed.value, -6796525, "the parsed value carries its sign");
  assert.equal(countUpText(parsed, -1234.4), money("-1234"));
  assert.equal(countUpText(parsed, 1234.4), money("1234"), "a positive frame never renders a minus");
  assert.equal(countUpText(parsed, -0.4), money("0"), "a value rounding to zero renders no sign");
});

function fakeClock() {
  let time = 0;
  let queue = [];
  let nextHandle = 1;
  return {
    now: () => time,
    schedule: callback => { const handle = nextHandle++; queue.push([handle, callback]); return handle; },
    cancel: handle => { queue = queue.filter(([id]) => id !== handle); },
    advance: ms => { time += ms; const due = queue; queue = []; for (const [, callback] of due) callback(); },
    pending: () => queue.length,
  };
}

test("the count-up always finishes on the exact target, even if frames are skipped", () => {
  const clock = fakeClock();
  const frames = [];
  let done = 0;
  startCountUp({ from: 0, to: 6796525, duration: COUNT_UP_MS, onFrame: value => frames.push(value), onDone: () => { done += 1; }, ...clock });
  clock.advance(100);
  clock.advance(300);
  assert.ok(frames.every((value, index) => index === 0 || value >= frames[index - 1]), "frames rise monotonically");
  assert.ok(frames.at(-1) > 0 && frames.at(-1) < 6796525, "mid-flight frames sit strictly between the ends");
  assert.equal(done, 0);
  clock.advance(10000);
  assert.equal(frames.at(-1), 6796525, "the last frame is the exact target");
  assert.equal(done, 1);
  assert.equal(clock.pending(), 0, "no further frames are scheduled");
});

test("a cancelled count-up emits nothing and never reports done", () => {
  const clock = fakeClock();
  const frames = [];
  let done = 0;
  const cancel = startCountUp({ from: 0, to: 500, duration: 800, onFrame: value => frames.push(value), onDone: () => { done += 1; }, ...clock });
  clock.advance(200);
  cancel();
  const emitted = frames.length;
  clock.advance(5000);
  assert.equal(frames.length, emitted);
  assert.equal(done, 0);
});

function runCountUp(fromText, toText) {
  const from = parseCountUpText(fromText);
  const to = parseCountUpText(toText);
  const clock = fakeClock();
  const frames = [];
  startCountUp({ from: from.value, to: to.value, duration: 800, onFrame: value => frames.push(value), onDone: () => {}, ...clock });
  for (let step = 0; step < 40; step += 1) clock.advance(25);
  return { to, frames };
}

for (const [label, fromAmount, toAmount] of [["positive to negative", "5000000", "-2000000"], ["negative to positive", "-3000000", "4500000"], ["negative to negative", "-6796525", "-1200000"], ["positive to positive", "1200000", "6796525"]]) {
  test(`count-up ${label} shows a sign that always matches the value on every frame`, () => {
    const { to, frames } = runCountUp(money(fromAmount), money(toAmount));
    assert.ok(frames.length > 3, "the tween renders intermediate frames");
    for (const value of frames) {
      const rendered = countUpText(to, value);
      const rounded = Math.round(value);
      assert.equal(rendered, money(String(rounded)), `${rendered} is the formatted value of ${rounded}`);
      assert.equal(rendered.startsWith("−"), rounded < 0, `${rendered} carries a minus only for negative values`);
    }
    assert.equal(countUpText(to, frames.at(-1)), money(toAmount), "the final frame equals the original text exactly");
  });
}

test("a count-up that crosses zero passes through zero instead of flipping the sign early", () => {
  const { to, frames } = runCountUp(money("5000000"), money("-2000000"));
  const rendered = frames.map(value => countUpText(to, value));
  assert.ok(!rendered.at(0).startsWith("−"), "the first frame is not already negative");
  assert.ok(rendered.at(-1).startsWith("−"), "ends negative");
  const firstNegative = rendered.findIndex(text => text.startsWith("−"));
  assert.ok(firstNegative > 0, "positive frames come first");
  assert.ok(rendered.slice(0, firstNegative).every(text => !text.startsWith("−")), "no negative frame precedes the crossing");
  assert.ok(rendered.slice(firstNegative).every(text => text.startsWith("−")), "no positive frame follows the crossing");
});

test("an explicit plus sign survives for positive frames and yields to the minus below zero", () => {
  const plus = parseCountUpText("+Rp 1.250.000");
  assert.equal(plus.value, 1250000);
  assert.equal(countUpText(plus, 800.2), "+Rp 800");
  assert.equal(countUpText(plus, 0.2), "Rp 0");
  assert.equal(countUpText(plus, -300), "−Rp 300");
});

test("counting down lands exactly on the lower target", () => {
  const clock = fakeClock();
  const frames = [];
  startCountUp({ from: 1000, to: 250, duration: 800, onFrame: value => frames.push(value), onDone: () => {}, ...clock });
  clock.advance(400);
  clock.advance(4000);
  assert.equal(frames.at(-1), 250);
});

test("charts draw in through the shared hook and only the donut stays static", () => {
  const charts = text("app/components/Charts.js");
  assert.equal((charts.match(/isAnimationActive=\{false\}/g) || []).length, 1, "only the donut keeps a hard-coded off switch");
  assert.match(charts.match(/export function CategoryDonutChart[\s\S]*?\n\}/)?.[0] ?? "", /<Pie [^>]*isAnimationActive=\{false\}/);
  for (const name of ["DashboardDailySpendingChart", "CycleSpendingPatternChart", "CyclePaceChart", "MonthlyCashflowChart", "CategoryRankingChart", "NetWorthHistoryChart"]) {
    const body = charts.match(new RegExp(`export function ${name}[\\s\\S]*?\\n\\}`))?.[0] ?? "";
    assert.match(body, /useChartMotion\(/, `${name} reads the shared motion hook`);
    assert.match(body, /\{\.\.\.motion\}/, `${name} spreads the motion props onto its series`);
  }
});

test("the chart hook stops animating a dataset when its series reports the draw finished, not on a timer, and never under reduced motion", () => {
  const hook = text("app/lib/useMotion.js");
  const body = hook.match(/export function useChartMotion[\s\S]*?\n\}/)?.[0] ?? "";
  assert.match(body, /usePrefersReducedMotion\(\)/);
  assert.match(body, /chartMotionProps\(!reduced && settledKey !== key, settle\)/);
  assert.match(body, /useCallback\(\(\) => setSettledKey\(key\), \[key\]\)/);
  assert.doesNotMatch(body, /setTimeout/, "a slow first paint must not use up the draw-in");
});

test("hero figures count up through one component that renders the final text for assistive tech", () => {
  const component = text("app/components/CountUp.js");
  assert.match(component, /aria-hidden="true"/);
  assert.match(component, /className="visually-hidden">\{text\}/);
  assert.match(component, /if \(!animating\) return text;/);
  for (const [file, patterns] of Object.entries({
    "app/page.js": [/home-hero-amount"><CountUp text=\{money\(overview\?\.netCashflow\)\}/, /<dd><CountUp text=\{money\(overview\?\.unallocatedSurplus\)\}/, /<CountUp text=\{money\(latestWealth\.netWorthIdr\)\}/],
    "app/analytics/CycleDashboard.js": [/<dd><CountUp text=\{cell\.value\}\/><\/dd>/],
    "app/wealth/WealthWorkspace.js": [/<CountUp text=\{money\(latest\.netWorthIdr\)\}/, /<CountUp text=\{money\(latest\.assetTotalIdr\)\}/, /<CountUp text=\{money\(latest\.liabilityTotalIdr\)\}/],
  })) {
    for (const pattern of patterns) assert.match(text(file), pattern, `${file} ${pattern}`);
  }
});

test("css bars draw in from a keyed element so only a dataset change replays them", () => {
  const motion = text("app/styles/15-motion.css");
  assert.match(motion, /@keyframes bar-grow-x/);
  assert.match(motion, /@keyframes bar-grow-y/);
  assert.match(text("app/components/CategoryBars.js"), /<span key=\{`\$\{row\.id \|\| row\.name\}:\$\{row\.amount\}`\}/);
  assert.match(text("app/analytics/CycleWhere.js"), /<div key=\{segments\.map/);
  assert.match(text("app/analytics/CycleWhere.js"), /<span key=\{`\$\{column\.start\}:\$\{column\.expense\}/);
  assert.match(text("app/page.js"), /<span key=\{share\} style/);
});
