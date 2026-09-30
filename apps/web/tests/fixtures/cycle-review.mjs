// Synthetic only. Mirrors cycle-review-v1; no household/provider evidence.
export function cycleFacts(start = "2026-09-01") {
  const closed = start === "2026-08-26";
  const period = { kind: "SALARY_CYCLE", start: closed ? "2026-08-26" : "2026-09-01", end: closed ? "2026-09-01" : null, measuredUntil: closed ? "2026-09-01" : "2026-09-07", state: closed ? "CLOSED" : "ACTIVE", configured: true };
  const previous = { kind: "SALARY_CYCLE", start: closed ? "2026-07-26" : "2026-08-26", end: closed ? "2026-08-26" : "2026-09-01", measuredUntil: closed ? "2026-08-26" : "2026-09-01", state: "CLOSED", configured: true };
  const change = (id, name, amount, previous, median3, deltaVsPrevious, deltaVsMedian3, relative, share, contribution) => ({
    id, name, amount, previous, median3, deltaVsPrevious, deltaVsMedian3, relativeDeltaVsPrevious: relative, relativeDeltaVsMedian3: null,
    shareOfExpense: share, contributionToExpenseChange: contribution, count: 4,
  });
  const groceries = change("11111111-1111-4111-8111-111111111111", "Belanja rumah", "2100000", "1400000", "1800000", "700000", "300000", "0.5000", "0.5000", "0.5000");
  groceries.merchants = [change("22222222-2222-4222-8222-222222222222", "Pasar Keluarga", "2100000", "1400000", "1800000", "700000", "300000", "0.5000", "0.5000", "0.5000")];
  groceries.transactions = [{ id: "33333333-3333-4333-8333-333333333333", type: "EXPENSE", merchant: "Pasar Keluarga", amount: "1200000", transactionAt: `${period.start}T09:00:00+07:00` }, { id: "44444444-4444-4444-8444-444444444444", type: "REFUND", merchant: "Pasar Keluarga", amount: "300000", transactionAt: `${period.start}T10:00:00+07:00` }];
  const dining = change("55555555-5555-4555-8555-555555555555", "Makan di luar", "1400000", "800000", "1350000", "600000", "50000", "0.7500", "0.3333", "0.4286");
  dining.merchants = []; dining.transactions = [];
  const transport = change("66666666-6666-4666-8666-666666666666", "Transportasi", "700000", "600000", "650000", "100000", "50000", "0.1667", "0.1667", "0.0714");
  transport.merchants = []; transport.transactions = [];
  const amounts = ["900000", "700000", "1100000", "300000", "1000000", "200000"];
  const daily = amounts.map((expense, index) => {
    const date = new Date(`${period.start}T00:00:00Z`);
    date.setUTCDate(date.getUTCDate() + index);
    return { period: date.toISOString().slice(0, 10), expense, grossExpense: index ? expense : "1200000", refund: index ? "0" : "300000", income: index ? "0" : "12000000" };
  });
  return {
    version: "cycle-review-v1", generatedAt: "2026-09-06T12:00:00+07:00", period,
    cycles: [{ ...period, start: "2026-09-01", end: null, state: "ACTIVE", measuredUntil: "2026-09-07" }, { ...period, start: "2026-08-26", end: "2026-09-01", state: "CLOSED", measuredUntil: "2026-09-01" }],
    comparison: { mode: closed ? "FULL_CYCLE" : "ELAPSED_DAYS", previous, eligibleCycles: 3, median3Available: true,
      expense: change("", "", "4200000", "2800000", "3800000", "1400000", "400000", "0.5000", null, null) },
    cashflow: { income: "12000000", grossExpense: "4500000", refund: "300000", expense: "4200000", netCashflow: "7800000", savingsAllocated: "5000000", unallocatedSurplus: "2800000" },
    spendingShape: { days: 6, averageDailyExpense: "700000.00", peakDay: daily[2].period, peakExpense: "1100000", peakShareOfExpense: "0.2619", zeroSpendDays: 0 },
    daily, categoryChanges: [groceries, dining, transport], merchantDrivers: groceries.merchants,
    memberAttribution: [{ id: "member-1", name: "Dina memulai", amount: "2000000", count: 4 }, { id: "member-2", name: "Rafi memulai", amount: "2200000", count: 5 }],
    savingsDestinations: [{ id: "account-1", name: "Tabungan keluarga", amount: "5000000", count: 1 }],
    wealth: { previous: { id: "snapshot-1", observedAt: "2026-08-25T10:00:00+07:00", netWorth: "40000000", ageDays: 12 },
      current: { id: "snapshot-2", observedAt: closed ? "2026-08-31T10:00:00+07:00" : "2026-09-06T10:00:00+07:00", netWorth: "49000000", ageDays: 1 },
      netWorthChange: "9000000", confirmedCashflow: "7800000", valuationAndOtherChange: "1200000" },
    dataQuality: [{ kind: "OPEN_REVIEWS", count: 2, impact: "ANALYSIS_PARTIAL", action: "/inbox" }],
  };
}

export const cycleCommentary = {
  id: "synthetic-commentary", status: "SUCCEEDED", historical: false, promptVersion: "cycle-analyst-v3",
  text: "Belanja rumah menyumbang setengah selisih pengeluaran dari siklus sebelumnya. Makan di luar lebih tinggi dari siklus sebelumnya, tetapi dekat dengan median tiga siklus. Bukti merchant dan transaksi dapat diperiksa sebelum dibahas bersama.",
  metrics: { period_kind: "CURRENT_CYCLE", period_start: "2026-09-01" }, completedAt: "2026-09-06T12:00:00+07:00",
};
