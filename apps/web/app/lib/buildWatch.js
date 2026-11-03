// Polls the deployed build id and reports when it differs from the one this
// page loaded (#413). The service worker is a pass-through with skipWaiting, so
// there is never a waiting worker to watch: the signal has to be a comparison
// between the id baked into this bundle and the id the server serves now.
//
// No React here, so the behaviour is testable with a stub document and timers.

// Ten minutes. A tab is often left open on a phone all day; this makes at most
// 144 requests a day of six bytes each for a tab someone is looking at, and a
// release reaches it within ten minutes, which is fast enough for an app whose
// pages refetch on navigation anyway.
export const POLL_INTERVAL_MS = 10 * 60 * 1000;

// Only the exact id shape counts. A captive portal's HTML, a truncated body, or
// an error page must never look like a new release.
export function deployedIdFrom(body) {
  const id = String(body ?? "").trim();
  return /^[0-9a-f]{8,64}$/.test(id) ? id : null;
}

export async function fetchDeployedId(fetchImpl = globalThis.fetch) {
  const response = await fetchImpl("/build-id.txt", { cache: "no-store" });
  if (!response.ok) return null;
  return deployedIdFrom(await response.text());
}

const isHidden = doc => doc?.visibilityState === "hidden";

export function startBuildWatch({
  ownId,
  fetchDeployedId: readDeployedId,
  onUpdate,
  document: doc = globalThis.document,
  interval = POLL_INTERVAL_MS,
  setTimer = (fn, ms) => setTimeout(fn, ms),
  clearTimer = id => clearTimeout(id),
}) {
  let stopped = false;
  let dismissed = false;
  let timer = null;
  let inFlight = false;

  function clear() {
    if (timer === null) return;
    clearTimer(timer);
    timer = null;
  }

  // A hidden tab never polls: a phone in a pocket should not spend radio on a
  // notice nobody can see.
  async function check() {
    if (stopped || dismissed || inFlight || isHidden(doc)) return;
    inFlight = true;
    try {
      const deployed = await readDeployedId();
      // Offline, a 502 and an empty body all arrive as null; no answer is not
      // an update.
      if (!stopped && !dismissed && deployed && deployed !== ownId) onUpdate();
    } catch {
      // A failed fetch is not an update, and it is not an error worth showing.
    } finally {
      inFlight = false;
    }
  }

  function schedule() {
    clear();
    if (stopped || dismissed || isHidden(doc)) return;
    timer = setTimer(() => {
      timer = null;
      void check().finally(schedule);
    }, interval);
  }

  function onVisibilityChange() {
    if (stopped || dismissed) return;
    if (isHidden(doc)) {
      clear();
      return;
    }
    // Coming back to a tab that was away for a while is exactly when a release
    // is most likely to have happened: check once now, then resume the cycle.
    clear();
    void check().finally(schedule);
  }

  doc?.addEventListener?.("visibilitychange", onVisibilityChange);
  // Check once at mount, after the page has painted: every route mounts a new
  // shell, so a timer that starts fresh here would be reset by an active user
  // before it ever fired. The comparison is seeded with the page's own id, so
  // this first check cannot announce an update by itself. Navigating then costs
  // one small conditional request, and a hidden document still checks nothing.
  void check();
  schedule();

  return {
    check,
    // Dismissal lasts this page load only and is never persisted: a user who
    // dismissed one release must still be told about the next one.
    dismiss() {
      dismissed = true;
      clear();
      doc?.removeEventListener?.("visibilitychange", onVisibilityChange);
    },
    stop() {
      stopped = true;
      clear();
      doc?.removeEventListener?.("visibilitychange", onVisibilityChange);
    },
  };
}
