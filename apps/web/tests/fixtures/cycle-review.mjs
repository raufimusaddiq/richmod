// Synthetic only. Mirrors cycle-review-v1; no household/provider evidence.
export function cycleFacts(start = "2026-09-01") {
  const closed = start === "2026-08-26";
  const september25 = start === "2026-09-25";
  const period = { kind: "SALARY_CYCLE", start: closed ? "2026-08-26" : september25 ? "2026-09-25" : "2026-09-01", end: closed ? "2026-09-01" : null, measuredUntil: closed ? "2026-09-01" : september25 ? "2026-10-02" : "2026-09-07", state: closed ? "CLOSED" : "ACTIVE", configured: true };
  const previous = { kind: "SALARY_CYCLE", start: closed ? "2026-07-26" : "2026-08-26", end: closed ? "2026-08-26" : "2026-09-01", measuredUntil: closed ? "2026-08-26" : "2026-09-01", state: "CLOSED", configured: true };
  const change = (id, name, amount, previous, median3, deltaVsPrevious, deltaVsMedian3, relative, share, contribution) => ({
    id, name, amount, previous, median3, deltaVsPrevious, deltaVsMedian3, relativeDeltaVsPrevious: relative, relativeDeltaVsMedian3: null,
    shareOfExpense: share, contributionToExpenseChange: contribution, count: 4,
    previousFullCycle: previous, deltaVsPreviousFullCycle: deltaVsPrevious, relativeDeltaVsPreviousFullCycle: relative,
  });
  const groceries = change("11111111-1111-4111-8111-111111111111", "Belanja rumah", "2100000", "1400000", "1800000", "700000", "300000", "0.5000", "0.5000", "0.5000");
  groceries.merchants = [change("22222222-2222-4222-8222-222222222222", "Pasar Keluarga", "2100000", "1400000", "1800000", "700000", "300000", "0.5000", "0.5000", "0.5000")];
  groceries.transactions = [{ id: "33333333-3333-4333-8333-333333333333", type: "EXPENSE", merchant: "Pasar Keluarga", amount: "1200000", transactionAt: `${period.start}T09:00:00+07:00` }, { id: "44444444-4444-4444-8444-444444444444", type: "REFUND", merchant: "Pasar Keluarga", amount: "300000", transactionAt: `${period.start}T10:00:00+07:00` }];
  const dining = change("55555555-5555-4555-8555-555555555555", "Makan di luar", "1400000", "800000", "1350000", "600000", "50000", "0.7500", "0.3333", "0.4286");
  dining.merchants = []; dining.transactions = [];
  const transport = change("66666666-6666-4666-8666-666666666666", "Transportasi", "700000", "600000", "650000", "100000", "50000", "0.1667", "0.1667", "0.0714");
  transport.merchants = []; transport.transactions = [];
  if (september25) {
    transport.name = "Tempat Tinggal";
    transport.amount = "1950000"; transport.previous = "0";
    transport.deltaVsPrevious = "1950000"; transport.relativeDeltaVsPrevious = null;
    transport.previousFullCycle = "1950000"; transport.deltaVsPreviousFullCycle = "0"; transport.relativeDeltaVsPreviousFullCycle = "0.0000";
    previous.start = "2026-08-24"; previous.end = "2026-09-25"; previous.measuredUntil = "2026-08-31";
  }
  const amounts = ["900000", september25 ? "0" : "700000", "1100000", "300000", "1000000", "200000"];
  const daily = amounts.map((expense, index) => {
    const date = new Date(`${period.start}T00:00:00Z`);
    date.setUTCDate(date.getUTCDate() + index);
    return { period: date.toISOString().slice(0, 10), expense, grossExpense: index ? expense : "1200000", refund: index ? "0" : "300000", income: index ? "0" : "12000000" };
  });
  if (september25) daily.push({ period: "2026-10-01", expense: "1950000", grossExpense: "1950000", refund: "0", income: "0" });
  let runningTotal = 0n; // the API serves cumulativeExpense on every daily row
  for (const row of daily) { runningTotal += BigInt(row.expense); row.cumulativeExpense = String(runningTotal); }
  const facts = {
    version: "cycle-review-v1", generatedAt: "2026-09-06T12:00:00+07:00", period,
    cycles: [{ ...period, start: "2026-09-01", end: null, state: "ACTIVE", measuredUntil: "2026-09-07" }, { ...period, start: "2026-08-26", end: "2026-09-01", state: "CLOSED", measuredUntil: "2026-09-01" }],
    comparison: { mode: closed ? "FULL_CYCLE" : "ELAPSED_DAYS", previous, previousFullCycle: { ...previous, measuredUntil: previous.end }, eligibleCycles: 3, median3Available: true,
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
  if (september25) {
    facts.generatedAt = "2026-10-01T12:00:00+07:00";
    facts.cycles = [period, previous];
    facts.comparison.eligibleCycles = 1; facts.comparison.median3Available = false;
    facts.comparison.expense = { ...facts.comparison.expense, amount: "5450000", previous: "2200000", deltaVsPrevious: "3250000", relativeDeltaVsPrevious: "1.4773", previousFullCycle: "4150000", deltaVsPreviousFullCycle: "1300000", relativeDeltaVsPreviousFullCycle: "0.3133", median3: null, deltaVsMedian3: null };
    facts.cashflow = { ...facts.cashflow, grossExpense: "5750000", expense: "5450000", netCashflow: "6550000", unallocatedSurplus: "1550000" };
    facts.spendingShape = { days: 7, averageDailyExpense: "778571.43", peakDay: "2026-10-01", peakExpense: "1950000", peakShareOfExpense: "0.3578", zeroSpendDays: 1 };
    for (const item of facts.categoryChanges) { item.median3 = null; item.deltaVsMedian3 = null; item.relativeDeltaVsMedian3 = null; }
    groceries.shareOfExpense = "0.3853"; dining.shareOfExpense = "0.2569"; transport.shareOfExpense = "0.3578";
    groceries.contributionToExpenseChange = "0.2154"; dining.contributionToExpenseChange = "0.1846"; transport.contributionToExpenseChange = "0.6000";
  }
  // Ledger series (history[]), oldest first, as the API serves it: whole-cycle totals
  // plus the server-computed change from the preceding entry.
  const earlier = [["2026-04-26", "2026-05-26", "12000000", "7400000"], ["2026-05-26", "2026-06-26", "12000000", "8100000"], ["2026-06-26", "2026-07-26", "12500000", "7900000"], ["2026-07-26", "2026-08-26", "12500000", "7200000"]].filter(([, end]) => end <= previous.start);
  const rows = [...earlier.map(([start, end, income, expense]) => ({ start, end, state: "CLOSED", income, expense })),
    { start: previous.start, end: previous.end, state: "CLOSED", income: "12000000", expense: facts.comparison.expense.previousFullCycle },
    { start: facts.period.start, end: facts.period.end, state: facts.period.state, income: facts.cashflow.income, expense: facts.cashflow.expense }];
  facts.history = rows.map((row, index) => ({ start: row.start, end: row.end, measuredUntil: row.end ?? facts.period.measuredUntil, state: row.state, income: row.income, grossExpense: row.expense, refund: "0", expense: row.expense,
    netCashflow: String(BigInt(row.income) - BigInt(row.expense)), savingsAllocated: "0", expenseDelta: index ? String(BigInt(row.expense) - BigInt(rows[index - 1].expense)) : null }));
  facts.categoryHistory = { cycleStarts: facts.history.map(item => item.start), rows: [], other: { amounts: facts.history.map(item => item.expense) } };
  return facts;
}

export const cycleCommentary = {
  id: "synthetic-commentary", status: "SUCCEEDED", historical: false, promptVersion: "cycle-analyst-v5", createdAt: "2026-09-06T11:59:59+07:00",
  text: "Belanja rumah menyumbang setengah selisih pengeluaran dari siklus sebelumnya. Makan di luar lebih tinggi dari siklus sebelumnya, tetapi dekat dengan median tiga siklus. Bukti merchant dan transaksi dapat diperiksa sebelum dibahas bersama.",
  metrics: { period_kind: "CURRENT_CYCLE", period_start: "2026-09-01", period_end: "2026-09-07" }, completedAt: "2026-09-06T12:00:00+07:00",
};

export function stableCycleFacts() {
  const facts = cycleFacts("2026-08-26");
  const category = { ...facts.categoryChanges[0], amount: "1400000", previous: "1400000", previousFullCycle: "1400000", deltaVsPreviousFullCycle: "0", relativeDeltaVsPreviousFullCycle: "0", median3: "1400000", deltaVsPrevious: "0", deltaVsMedian3: "0", relativeDeltaVsPrevious: "0", relativeDeltaVsMedian3: "0", shareOfExpense: "1", contributionToExpenseChange: null, count: 6, merchants: [], transactions: [] };
  facts.categoryChanges = [category];
  facts.comparison.expense = { ...category, id: "", name: "" };
  facts.cashflow = { income: "1400000", grossExpense: "1400000", refund: "0", expense: "1400000", netCashflow: "0", savingsAllocated: "0", unallocatedSurplus: "0" };
  facts.daily = facts.daily.map((day, index) => ({ ...day, expense: index < 4 ? "200000" : "300000", grossExpense: index < 4 ? "200000" : "300000", refund: "0", income: index === 0 ? "1400000" : "0" }));
  let stableTotal = 0n; // keep the served running total consistent with the rewritten days
  for (const day of facts.daily) { stableTotal += BigInt(day.expense); day.cumulativeExpense = String(stableTotal); }
  const last = facts.history.at(-1); // and the ledger's selected column with the rewritten cashflow
  Object.assign(last, { income: "1400000", grossExpense: "1400000", expense: "1400000", netCashflow: "0" });
  last.expenseDelta = String(BigInt(last.expense) - BigInt(facts.history.at(-2).expense));
  facts.categoryHistory.other.amounts[facts.history.length - 1] = last.expense;
  facts.spendingShape = { days: 6, averageDailyExpense: "233333.33", peakDay: facts.daily[4].period, peakExpense: "300000", peakShareOfExpense: "0.2143", zeroSpendDays: 0 };
  facts.merchantDrivers = []; facts.memberAttribution = [{ name: "Shared / unattributed", amount: "1400000", count: 6 }]; facts.savingsDestinations = [];
  facts.wealth.current.netWorth = facts.wealth.previous.netWorth;
  facts.wealth.netWorthChange = "0"; facts.wealth.confirmedCashflow = "0"; facts.wealth.valuationAndOtherChange = "0";
  facts.dataQuality = [];
  return facts;
}
