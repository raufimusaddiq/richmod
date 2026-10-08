function metricsOf(insight) {
  if (!insight?.metrics) return {};
  if (typeof insight.metrics === "string") {
    try { return JSON.parse(insight.metrics); } catch { return {}; }
  }
  return insight.metrics;
}

export function insightPeriod(insight) {
  const metrics = metricsOf(insight);
  return { start: metrics.period_start, measuredUntil: metrics.period_end };
}

export function selectCycleInsight(insights = [], cycle = {}) {
  const start = cycle.cycleStart || cycle.start;
  if (!start) return null;
  return insights
    .filter(item => {
      const metrics = metricsOf(item);
      const end = metrics.period_end;
      const date = new Date(`${end}T00:00:00Z`);
      const olderSnapshot = cycle.state === "ACTIVE" && typeof end === "string" && !Number.isNaN(date.getTime())
        && date.toISOString().slice(0, 10) === end && end > start && end < cycle.measuredUntil;
      return item.historical !== true && ["CURRENT_CYCLE", "SALARY_CYCLE"].includes(metrics.period_kind) && metrics.period_start === start
        && (!cycle.measuredUntil || end === cycle.measuredUntil || olderSnapshot);
    })
    .sort((a, b) => new Date(b.createdAt || 0) - new Date(a.createdAt || 0))[0] || null;
}

export async function pollInsight({ insightId, load, onUpdate = () => {}, signal, cycle, attempts = 90, wait = abortableDelay }) {
  for (let attempt = 0; attempt < attempts; attempt += 1) {
    if (signal?.aborted) throw new DOMException("Aborted", "AbortError");
    const selected = (await load(signal)).find(item => item.id === insightId) || null;
    if (selected && cycle && !selectCycleInsight([selected], cycle)) throw new Error("insight cutoff mismatch");
    if (selected) onUpdate(selected);
    if (selected?.status === "SUCCEEDED" || selected?.status === "FAILED") return selected;
    if (attempt < attempts - 1) await wait(5000, signal);
  }
  throw new Error("insight polling timeout");
}

function abortableDelay(milliseconds, signal) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(resolve, milliseconds);
    signal?.addEventListener("abort", () => { clearTimeout(timer); reject(new DOMException("Aborted", "AbortError")); }, { once: true });
  });
}
