export const CHART_DRAW_MS = 700;
export const COUNT_UP_MS = 800;
export const CHART_SETTLE_MS = 120;
export const CHART_EASING = "ease-out";
export const REDUCED_MOTION_QUERY = "(prefers-reduced-motion: reduce)";

function mediaQuery() {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") return null;
  return window.matchMedia(REDUCED_MOTION_QUERY);
}

export function prefersReducedMotion() {
  const query = mediaQuery();
  return query ? query.matches : true;
}

export function subscribeReducedMotion(callback) {
  const query = mediaQuery();
  if (!query) return () => {};
  if (typeof query.addEventListener === "function") {
    query.addEventListener("change", callback);
    return () => query.removeEventListener("change", callback);
  }
  query.addListener?.(callback);
  return () => query.removeListener?.(callback);
}

export function easeOutCubic(progress) {
  const clamped = Math.min(1, Math.max(0, progress));
  return 1 - Math.pow(1 - clamped, 3);
}

export function chartMotionProps(active) {
  return { isAnimationActive: active, animationBegin: 0, animationDuration: CHART_DRAW_MS, animationEasing: CHART_EASING };
}

export function datasetKey(data) {
  try { return JSON.stringify(data ?? null); } catch { return String(Date.now()); }
}

const grouped = new Intl.NumberFormat("id-ID", { maximumFractionDigits: 0 });
const NUMBER_TOKEN = /\d+(?:\.\d{3})*/;

export function parseCountUpText(text) {
  const source = String(text ?? "");
  const match = NUMBER_TOKEN.exec(source);
  if (!match) return null;
  const digits = match[0].replace(/\./g, "");
  const value = Number(digits);
  if (!Number.isSafeInteger(value)) return null;
  return { prefix: source.slice(0, match.index), suffix: source.slice(match.index + match[0].length), value };
}

export function countUpText(parsed, value) {
  return `${parsed.prefix}${grouped.format(Math.round(value))}${parsed.suffix}`;
}

export function countUpValue(from, to, progress) {
  if (progress >= 1) return to;
  return from + (to - from) * easeOutCubic(progress);
}

export function startCountUp({ from, to, duration = COUNT_UP_MS, onFrame, onDone, now = () => performance.now(), schedule = callback => requestAnimationFrame(callback), cancel = handle => cancelAnimationFrame(handle) }) {
  let handle = null;
  let stopped = false;
  const begin = now();
  const tick = () => {
    if (stopped) return;
    const progress = duration > 0 ? (now() - begin) / duration : 1;
    if (progress >= 1) {
      onFrame(to);
      onDone();
      return;
    }
    onFrame(countUpValue(from, to, progress));
    handle = schedule(tick);
  };
  handle = schedule(tick);
  return () => {
    stopped = true;
    if (handle != null) cancel(handle);
  };
}
