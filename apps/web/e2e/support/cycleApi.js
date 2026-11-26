import { reply } from "./api.js";
import { cycleCommentary, cycleFacts, earlyCycleFacts, stableCycleFacts } from "../../tests/fixtures/cycle-review.mjs";

export const cycleUser = { id: "synthetic-user", displayName: "Rafi", householdName: "Rumah keluarga", household: { role: "OWNER" } };

export function cycleReviewApi(api) {
  const flags = {
    aiAvailable: true,
    generationStatus: 202,
    olderCommentary: false,
    invalidFacts: false,
    merchantsFail: false,
    noSalary: false,
    stable: false,
    salaryCalls: 0,
    failDecisionSave: false,
    decisionsFail: false,
    advisory: false,
  };
  const notes = new Map([["2026-07-26", [{ id: "77777777-7777-4777-8777-777777777777", cycleStart: "2026-07-26", body: "Jaga lebih banyak kas likuid.", author: "Dina", authorUserId: "synthetic-user", createdAt: "2026-08-26T10:00:00+07:00" }]]]);

  api.use(request => {
    const { path, query, method, body: input } = request;
    if (path === "/api/v1/reviews") return [];
    if (path === "/api/v1/analytics/cycle-review") {
      flags.salaryCalls++;
      const cycleStart = query.get("cycle_start") || undefined;
      const body = flags.stable ? stableCycleFacts() : cycleStart === "2026-09-05" ? earlyCycleFacts(cycleStart) : cycleFacts(cycleStart);
      if (flags.advisory) body.dataQuality.push({ kind: "WEALTH_SNAPSHOT_BEFORE_CYCLE", count: 1, impact: "ANALYSIS_PARTIAL", action: "/wealth" });
      if (flags.noSalary) {
        body.period = { ...body.period, kind: "CALENDAR_MONTH", start: "2026-09-01", state: "ACTIVE", end: "2026-10-01", measuredUntil: "2026-09-07" };
        body.cycles = [];
        body.history = [];
        body.categoryHistory = { cycleStarts: [], rows: [], other: { amounts: [] } };
        body.dataQuality.push({ kind: "MISSING_SALARY_ANCHOR", count: 1, impact: "ANALYSIS_PARTIAL", action: "/settings" });
      }
      return reply(body, { status: flags.invalidFacts ? 503 : 200 });
    }
    if (path === "/api/v1/analytics/cycle-decisions") {
      if (method === "POST") {
        const status = flags.failDecisionSave ? 503 : 201;
        const body = { ...input, id: "88888888-8888-4888-8888-888888888888", author: "Rafi", authorUserId: "synthetic-user", createdAt: "2026-09-06T12:00:00+07:00" };
        if (!flags.failDecisionSave) notes.set(input.cycleStart, [...(notes.get(input.cycleStart) || []), body]);
        return reply(body, { status });
      }
      const start = query.get("cycle_start");
      const previous = cycleFacts(start).comparison.previous.start;
      return reply({ items: notes.get(start) || [], previous: notes.get(previous) || [], previousCycleStart: previous }, { status: flags.decisionsFail ? 500 : 200 });
    }
    if (path.endsWith("/revoke")) {
      const id = path.split("/").at(-2);
      for (const [start, items] of notes) notes.set(start, items.filter(item => item.id !== id));
      return { status: "REVOKED" };
    }
    if (path === "/api/v1/insights") {
      const items = [
        { ...cycleCommentary, historical: true, text: "Historical advice should never display." },
        { ...cycleCommentary, id: "stale-commentary", createdAt: "2026-09-05T12:00:00+07:00", text: "Stale cutoff must never display.", metrics: { ...cycleCommentary.metrics, period_end: "2026-09-06" } },
        flags.stable
          ? { ...cycleCommentary, text: "Tidak ada perubahan yang menonjol terhadap tiga siklus sebelumnya.", metrics: { period_kind: "SALARY_CYCLE", period_start: "2026-08-26", period_end: "2026-09-01" } }
          : flags.olderCommentary
            ? { ...cycleCommentary, text: "Older daily snapshot is visible.", metrics: { ...cycleCommentary.metrics, period_end: "2026-09-06" } }
            : { ...cycleCommentary },
      ];
      return reply(items, { status: flags.aiAvailable ? 200 : 503 });
    }
    if (path === "/api/v1/insights/generate") return reply({ id: cycleCommentary.id }, { status: flags.aiAvailable ? flags.generationStatus : 503 });
    if (path === "/api/v1/analytics/range-summary") {
      return { income: "24000000", expense: "7000000", refund: "0", netCashflow: "17000000", months: 2, averageMonthlyExpenseRounded: "3500000", peakMonth: "2026-09", peakMonthExpense: "4200000" };
    }
    if (path === "/api/v1/analytics/cashflow") {
      return [
        { period: "2026-07", income: "0", expense: "0", refund: "0", netCashflow: "0" },
        { period: "2026-08", income: "12000000", expense: "2800000", netCashflow: "9200000" },
        { period: "2026-09", income: "12000000", expense: "4200000", netCashflow: "7800000" },
        { period: "2026-10", income: "0", expense: "0", refund: "0", netCashflow: "0" },
      ];
    }
    if (path === "/api/v1/analytics/categories") return cycleFacts().categoryChanges;
    if (path.startsWith("/api/v1/wealth/")) return /snapshots\/latest|summary|current-cycle-savings/.test(path) ? null : [];
    if (path === "/api/v1/analytics/merchants") return reply(cycleFacts().merchantDrivers, { status: flags.merchantsFail ? 500 : 200 });
    if (path === "/api/v1/analytics/members") return cycleFacts().memberAttribution;
    if (path.startsWith("/api/v1/transactions")) {
      const transactions = cycleFacts(query.get("cycle") || undefined).categoryChanges[0].transactions.map(item => ({ ...item, merchantName: item.merchant, categoryName: "Belanja rumah", status: "CONFIRMED" }));
      const exact = query.get("id");
      if (path === "/api/v1/transactions") return exact ? transactions.filter(item => item.id === exact) : transactions;
      if (/\/evidence$|\/audit$/.test(path)) return [];
      return transactions.find(item => path.endsWith(item.id)) || transactions[0];
    }
    return undefined;
  });

  return flags;
}
