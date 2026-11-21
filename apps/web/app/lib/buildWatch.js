export const POLL_INTERVAL_MS = 10 * 60 * 1000;

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

  async function check() {
    if (stopped || dismissed || inFlight || isHidden(doc)) return;
    inFlight = true;
    try {
      const deployed = await readDeployedId();
      if (!stopped && !dismissed && deployed && deployed !== ownId) onUpdate();
    } catch {
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
    clear();
    void check().finally(schedule);
  }

  doc?.addEventListener?.("visibilitychange", onVisibilityChange);
  void check();
  schedule();

  return {
    check,
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
