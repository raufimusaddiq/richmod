// Polls the deployed build id and reports when it differs from the one this page
// loaded. The service worker is a pass-through with skipWaiting, so there is no
// waiting worker to watch: the signal is a comparison between the id baked into this
// bundle and the id the server serves now.
//
// No React here, so the behaviour is testable with a stub document and timers.

// Ten minutes: at most 144 six-byte requests a day for a tab left open, and a release
// reaches it quickly enough for an app whose pages refetch on navigation anyway.
export const POLL_INTERVAL_MS = 10 * 60 * 1000;

// Only the exact id shape counts; a captive portal's HTML, a truncated body or an error
// page must never look like a new release.
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

  // A hidden tab never polls; a phone in a pocket should not spend radio on a notice
  // nobody can see.
  async function check() {
    if (stopped || dismissed || inFlight || isHidden(doc)) return;
    inFlight = true;
    try {
      const deployed = await readDeployedId();
      // Offline, a 502 and an empty body all arrive as null; no answer is not an update.
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
    // A tab coming back after a while is when a release is most likely: check once, then
    // resume the cycle.
    clear();
    void check().finally(schedule);
  }

  doc?.addEventListener?.("visibilitychange", onVisibilityChange);
  // Check once at mount, after paint: every route mounts a new shell, so a fresh timer
  // would be reset by an active user before it fired. The comparison is seeded with the
  // page's own id, so this check cannot announce an update by itself.
  void check();
  schedule();

  return {
    check,
    // Dismissal lasts this page load only, so a dismissed release does not hide the next one.
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
