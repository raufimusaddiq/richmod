export function completenessLabel(value) {
  const ratio = Number(value || 0);
  if (ratio >= 0.9) return "Tinggi";
  if (ratio >= 0.7) return "Cukup";
  return "Perlu dilengkapi";
}

function metricsOf(insight) {
  if (!insight?.metrics) return {};
  if (typeof insight.metrics === "string") {
    try { return JSON.parse(insight.metrics); } catch { return {}; }
  }
  return insight.metrics;
}

export function selectCycleInsight(insights = [], cycle = {}) {
  const start = cycle.cycleStart || cycle.start;
  if (!start) return null;
  return insights
    .filter(item => {
      const metrics = metricsOf(item);
      return metrics.period_kind === "CURRENT_CYCLE" && metrics.period_start === start;
    })
    .sort((a, b) => new Date(b.createdAt || 0) - new Date(a.createdAt || 0))[0] || null;
}

export function insightQuality(insight) {
  const value = insight?.dataCompleteness ?? metricsOf(insight).data_completeness;
  return { value: Number(value || 0), label: completenessLabel(value) };
}

export async function pollInsight({ insightId, load, onUpdate = () => {}, signal, attempts = 15, wait = abortableDelay }) {
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    if (signal?.aborted) throw new DOMException("Aborted", "AbortError");
    const selected = (await load(signal)).find(item => item.id === insightId) || null;
    if (selected) onUpdate(selected);
    if (selected?.status === "SUCCEEDED" || selected?.status === "FAILED") return selected;
    if (attempt < attempts - 1) await wait(1800, signal);
  }
  throw new Error("insight polling timeout");
}

export function abortableDelay(milliseconds, signal) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(resolve, milliseconds);
    signal?.addEventListener("abort", () => { clearTimeout(timer); reject(new DOMException("Aborted", "AbortError")); }, { once: true });
  });
}
