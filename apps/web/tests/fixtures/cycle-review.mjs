function daysBetween(start, end) {
  if (!start || !end) return null;
  const from = Date.parse(`${start}T00:00:00Z`);
  const to = Date.parse(`${end}T00:00:00Z`);
  if (Number.isNaN(from) || Number.isNaN(to) || to <= from) return null;
  return Math.round((to - from) / 86400000);
}

function perDayRounded(amount, days) {
  if (!days || days <= 0 || amount == null) return null;
  return String(Math.round(Number(amount) / days));
}

function rankingFor(facts) {
  const ordered = facts.categoryChanges.filter(item => BigInt(item.amount) !== 0n).sort((a, b) => (BigInt(b.amount) > BigInt(a.amount) ? 1 : BigInt(b.amount) < BigInt(a.amount) ? -1 : 0))
    .map(item => ({ id: item.id || null, name: item.name, amount: item.amount, count: item.count ?? 0, shareOfExpense: item.shareOfExpense ?? null, baselineAmount: item.previous ?? null, deltaVsBaseline: item.deltaVsPrevious ?? null, relativeDeltaVsBaseline: item.relativeDeltaVsPrevious ?? null }));
  const categorized = ordered.filter(item => item.id != null && BigInt(item.amount) > 0n);
  const rows = categorized.slice(0, 8);
  const rowIds = new Set(rows.map(row => row.id));
  const otherRows = ordered.filter(item => !item.id || !rowIds.has(item.id));
  const listed = rows.reduce((sum, row) => sum + BigInt(row.amount), 0n);
  return { baseline: facts.comparison.mode === "ELAPSED_DAYS" ? "SAME_DAY" : "PREVIOUS_FULL", rows,
    other: { name: "Kategori lain", amount: String(BigInt(facts.cashflow.expense) - listed), count: otherRows.reduce((sum, row) => sum + row.count, 0), categoryCount: otherRows.length, shareOfExpense: null, baselineAmount: null, deltaVsBaseline: null, relativeDeltaVsBaseline: null, rows: otherRows } };
}

function merchantRankingFor(facts) {
  const payees = new Map();
  for (const change of facts.categoryChanges) for (const merchant of change.merchants || []) {
    const key = merchant.id || merchant.name;
    const entry = payees.get(key) || { id: merchant.id || "", name: merchant.name, amount: 0n, count: 0, previous: 0n };
    entry.amount += BigInt(merchant.amount); entry.count += merchant.count ?? 0; entry.previous += BigInt(merchant.previous ?? 0);
    payees.set(key, entry);
  }
  const rows = [...payees.values()].sort((a, b) => (b.amount > a.amount ? 1 : b.amount < a.amount ? -1 : 0)).slice(0, 10)
    .map(entry => ({ id: entry.id, name: entry.name, kind: entry.id ? "MERCHANT" : "COUNTERPARTY", amount: String(entry.amount), count: entry.count, previous: String(entry.previous), deltaVsPrevious: String(entry.amount - entry.previous), shareOfExpense: facts.cashflow.expense === "0" ? null : String(entry.amount) / facts.cashflow.expense }));
  const listed = rows.reduce((sum, row) => sum + BigInt(row.amount), 0n);
  const listedPrevious = rows.reduce((sum, row) => sum + BigInt(row.previous), 0n);
  const previous = facts.comparison.expense.previous ?? null;
  return {
    mode: facts.comparison.mode,
    rows,
    other: { amount: String(BigInt(facts.cashflow.expense) - listed), count: 0, merchantCount: Math.max(0, payees.size - rows.length), previous: previous == null ? null : String(BigInt(previous) - listedPrevious), deltaVsPrevious: previous == null ? null : String(BigInt(facts.cashflow.expense) - listed - (BigInt(previous) - listedPrevious)) },
    total: { amount: facts.cashflow.expense, count: 0, previous, deltaVsPrevious: previous == null ? null : String(BigInt(facts.cashflow.expense) - BigInt(previous)) },
  };
}

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
  let runningTotal = 0n;
  for (const row of daily) { runningTotal += BigInt(row.expense); row.cumulativeExpense = String(runningTotal); }
  const expenseComp = change("", "", "4200000", "2800000", "3800000", "1400000", "400000", "0.5000", null, null);
  expenseComp.relativeDeltaVsMedian3 = "0.1053";
  const facts = {
    version: "cycle-review-v1", generatedAt: "2026-09-06T12:00:00+07:00", period,
    cycles: [{ ...period, start: "2026-09-01", end: null, state: "ACTIVE", measuredUntil: "2026-09-07" }, { ...period, start: "2026-08-26", end: "2026-09-01", state: "CLOSED", measuredUntil: "2026-09-01" }],
    comparison: { mode: closed ? "FULL_CYCLE" : "ELAPSED_DAYS", previous, previousFullCycle: { ...previous, measuredUntil: previous.end }, eligibleCycles: 3, median3Available: true,
      expense: expenseComp },
    cashflow: { income: "12000000", grossExpense: "4500000", refund: "300000", expense: "4200000", netCashflow: "7800000", savingsAllocated: "5000000", unallocatedSurplus: "2800000" },
    spendingShape: { days: 6, averageDailyExpense: "700000.00", averageDailyExpenseRounded: "700000", peakDay: daily[2].period, peakExpense: "1100000", peakShareOfExpense: "0.2619", zeroSpendDays: 0 },
    daily, categoryChanges: [groceries, dining, transport], merchantDrivers: groceries.merchants,
    memberAttribution: [{ id: "", name: "Bersama / otomatis / belum diatribusikan", amount: "2000000", count: 4, kind: "AUTOMATIC", shareOfExpense: "0.4762" }, { id: "member-2", name: "Rafi memulai", amount: "2200000", count: 5, kind: "MEMBER", shareOfExpense: "0.5238" }],
    savingsDestinations: [{ id: "account-1", name: "Tabungan keluarga", amount: "5000000", count: 1 }],
    wealth: { previous: { id: "snapshot-1", observedAt: "2026-08-25T10:00:00+07:00", netWorth: "40000000", ageDays: 12 },
      current: { id: "snapshot-2", observedAt: closed ? "2026-08-31T10:00:00+07:00" : "2026-09-06T10:00:00+07:00", netWorth: "49000000", ageDays: 1 },
      netWorthChange: "9000000", confirmedCashflow: "7800000", valuationAndOtherChange: "1200000" },
    dataQuality: [{ kind: "OPEN_REVIEWS", count: 2, impact: "ANALYSIS_PARTIAL", action: "/inbox" }],
    previousCycle: null,
    takeaways: {
      verdict: closed ? "Rp 1.400.000 lebih tinggi dari siklus lalu (penuh)." : "Rp 1.400.000 lebih tinggi dari siklus lalu di hari ke-6.",
      where: "Belanja rumah menghabiskan 50% pengeluaran siklus ini.",
      history: closed ? "Per hari: Rp 700.000 (6 hari) vs Rp 90.323 siklus lalu (31 hari)." : "Rp 4,10 jt → Rp 4,20 jt (berjalan, hari ke-6)",
      changes: "Naik paling banyak Belanja rumah.",
      pace: "Hari ke-6: Rp 4,20 jt · siklus lalu Rp 2,80 jt di hari yang sama.",
      merchants: "Pasar Keluarga menjadi penerima terbesar siklus ini.",
    },
  };
  if (september25) {
    facts.generatedAt = "2026-10-01T12:00:00+07:00";
    facts.cycles = [period, previous];
    facts.comparison.eligibleCycles = 1; facts.comparison.median3Available = false;
    facts.comparison.expense = { ...facts.comparison.expense, amount: "5450000", previous: "2200000", deltaVsPrevious: "3250000", relativeDeltaVsPrevious: "1.4773", previousFullCycle: "4150000", deltaVsPreviousFullCycle: "1300000", relativeDeltaVsPreviousFullCycle: "0.3133", median3: null, deltaVsMedian3: null, relativeDeltaVsMedian3: null };
    facts.cashflow = { ...facts.cashflow, grossExpense: "5750000", expense: "5450000", netCashflow: "6550000", unallocatedSurplus: "1550000" };
    facts.spendingShape = { days: 7, averageDailyExpense: "778571.43", averageDailyExpenseRounded: "778571", peakDay: "2026-10-01", peakExpense: "1950000", peakShareOfExpense: "0.3578", zeroSpendDays: 1 };
    for (const item of facts.categoryChanges) { item.median3 = null; item.deltaVsMedian3 = null; item.relativeDeltaVsMedian3 = null; }
    groceries.shareOfExpense = "0.3853"; dining.shareOfExpense = "0.2569"; transport.shareOfExpense = "0.3578";
    groceries.contributionToExpenseChange = "0.2154"; dining.contributionToExpenseChange = "0.1846"; transport.contributionToExpenseChange = "0.6000";
    facts.takeaways.verdict = "Rp 3.250.000 lebih tinggi dari siklus lalu di hari ke-7.";
  }
  const earlier = [["2026-04-26", "2026-05-26", "12000000", "7400000"], ["2026-05-26", "2026-06-26", "12000000", "8100000"], ["2026-06-26", "2026-07-26", "12500000", "7900000"], ["2026-07-26", "2026-08-26", "12500000", "7200000"]].filter(([, end]) => end <= previous.start);
  const rows = [...earlier.map(([start, end, income, expense]) => ({ start, end, state: "CLOSED", income, expense })),
    { start: previous.start, end: previous.end, state: "CLOSED", income: "12000000", expense: facts.comparison.expense.previousFullCycle },
    { start: facts.period.start, end: facts.period.end, state: facts.period.state, income: facts.cashflow.income, expense: facts.cashflow.expense }];
  facts.history = rows.map((row, index) => {
    const measuredUntil = row.end ?? facts.period.measuredUntil;
    const days = daysBetween(row.start, measuredUntil) ?? facts.spendingShape.days;
    return {
      start: row.start,
      end: row.end,
      measuredUntil,
      state: row.state,
      days,
      averageDailyExpenseRounded: perDayRounded(row.expense, days),
      income: row.income,
      grossExpense: row.expense,
      refund: "0",
      expense: row.expense,
      netCashflow: String(BigInt(row.income) - BigInt(row.expense)),
      savingsAllocated: "0",
      expenseDelta: index ? String(BigInt(row.expense) - BigInt(rows[index - 1].expense)) : null,
    };
  });
  facts.pace = { previousFullCycle: Array.from({ length: 30 }, (_, index) => String((index + 1) * 100000)), median3: Array.from({ length: facts.daily.length }, (_, index) => String((index + 1) * 120000)) };
  const last = facts.history.length - 1;
  const columns = facts.history.map((item, index) => index === last ? facts.categoryChanges.map(change => change.amount) : [50n, 30n, 15n].map(percent => String(BigInt(item.expense) * percent / 100n)));
  facts.categoryHistory = {
    cycleStarts: facts.history.map(item => item.start),
    rows: facts.categoryChanges.map((change, row) => ({ id: change.id, name: change.name, amounts: columns.map(column => column[row]) })),
    other: { amounts: facts.history.map((item, index) => String(BigInt(item.expense) - columns[index].reduce((sum, value) => sum + BigInt(value), 0n))) },
  };
  const matrixTotals = facts.history.map(item => item.expense);
  const matrixDeltas = values => values.length > 1 ? String(BigInt(values.at(-1)) - BigInt(values.at(-2))) : null;
  facts.categoryHistory.totals = matrixTotals;
  facts.categoryHistory.totalDeltaVsPrevious = matrixDeltas(matrixTotals);
  for (const row of [...facts.categoryHistory.rows, facts.categoryHistory.other]) row.deltaVsPrevious = matrixDeltas(row.amounts);
  if (facts.period.state === "ACTIVE") {
    const elapsed = facts.spendingShape.days;
    const span = item => { const start = new Date(`${item.start}T00:00:00Z`); const end = new Date(`${item.end || item.measuredUntil}T00:00:00Z`); return Math.max(1, Math.round((end - start) / 86400000)); };
    const cut = facts.history.map(item => Math.min(item.state === "ACTIVE" ? elapsed : span(item), elapsed));
    const scale = (value, index) => String(BigInt(value) * BigInt(cut[index]) / BigInt(span(facts.history[index])));
    const sameRows = facts.categoryHistory.rows.map(row => ({ ...row, amounts: row.amounts.map(scale), deltaVsPrevious: null }));
    const sameOther = { amounts: facts.categoryHistory.other.amounts.map(scale), deltaVsPrevious: null };
    const sameTotals = matrixTotals.map(scale);
    for (const row of [...sameRows, sameOther]) row.deltaVsPrevious = matrixDeltas(row.amounts);
    facts.categoryHistory.sameDay = { elapsedDays: elapsed, measuredDays: cut, cycleStarts: facts.categoryHistory.cycleStarts, selectedIndex: facts.categoryHistory.cycleStarts.length - 1, rows: sameRows, other: sameOther, totals: sameTotals, totalDeltaVsPrevious: matrixDeltas(sameTotals) };
  }
  facts.merchantRanking = merchantRankingFor(facts);
  facts.categoryRanking = rankingFor(facts);
  return facts;
}

export function earlyCycleFacts(start = "2026-09-05") {
  const facts = cycleFacts("2026-09-01");
  facts.period = { kind: "SALARY_CYCLE", start, end: "2026-10-05", measuredUntil: "2026-09-07", state: "ACTIVE", configured: true };
  facts.cycles = [facts.period, { kind: "SALARY_CYCLE", start: "2026-08-26", end: "2026-09-01", measuredUntil: "2026-09-01", state: "CLOSED", configured: true }];
  facts.daily = [
    { period: "2026-09-05", expense: "1480000", grossExpense: "1480000", refund: "0", income: "12000000", cumulativeExpense: "1480000" },
    { period: "2026-09-06", expense: "210000", grossExpense: "210000", refund: "0", income: "0", cumulativeExpense: "1690000" },
  ];
  facts.cashflow = { income: "12000000", grossExpense: "1690000", refund: "0", expense: "1690000", netCashflow: "10310000", savingsAllocated: "0", unallocatedSurplus: "10310000" };
  facts.spendingShape = { days: 2, averageDailyExpense: "845000.00", averageDailyExpenseRounded: "845000", peakDay: "2026-09-05", peakExpense: "1480000", peakShareOfExpense: "0.8757", zeroSpendDays: 0 };
  facts.comparison.expense = { ...facts.comparison.expense, amount: "1690000", previous: "1980000", deltaVsPrevious: "-290000", relativeDeltaVsPrevious: "-0.1465", median3: null, deltaVsMedian3: null, relativeDeltaVsMedian3: null };
  facts.previousCycle = {
    start: "2026-08-26",
    end: "2026-09-01",
    days: 32,
    expense: "10960000",
    deltaVsPreviousFullCycle: "1090000",
    relativeDeltaVsPreviousFullCycle: "0.1104",
    savingsAllocated: "5000000",
    unallocatedSurplus: "540000",
  };
  facts.dataQuality = [{ kind: "WEALTH_SNAPSHOT_BEFORE_CYCLE", count: 1, impact: "ANALYSIS_PARTIAL", action: "/wealth" }];
  facts.takeaways = {
    verdict: "Baru 2 hari; sebagian besar tagihan rutin awal siklus.",
    where: "Tagihan & cicilan memakan 66% dari dua hari pertama.",
    history: "Siklus baru baru 2 hari; batang berjalan belum sebanding.",
    changes: "",
    pace: "Hari ke-2: Rp 1,69 jt · siklus lalu Rp 1,98 jt di hari yang sama.",
    merchants: "Dua hari pertama: tagihan rutin awal siklus.",
  };
  const emptyChange = (id, name, amount, share, count, merchants, transactions) => ({
    id,
    name,
    amount,
    previous: null,
    median3: null,
    deltaVsPrevious: null,
    deltaVsMedian3: null,
    relativeDeltaVsPrevious: null,
    relativeDeltaVsMedian3: null,
    shareOfExpense: share,
    contributionToExpenseChange: null,
    count,
    previousFullCycle: null,
    deltaVsPreviousFullCycle: null,
    relativeDeltaVsPreviousFullCycle: null,
    merchants,
    transactions,
  });
  const earlyBills = emptyChange(
    "66666666-6666-4666-8666-666666666666",
    "Tagihan & cicilan",
    "1120000",
    "0.6627",
    2,
    [
      { id: "m-pln", name: "PLN", amount: "640000", count: 1, previous: null, deltaVsPrevious: null },
      { id: "m-indihome", name: "IndiHome", amount: "480000", count: 1, previous: null, deltaVsPrevious: null },
    ],
    [
      { id: "tx-early-1", type: "EXPENSE", merchant: "PLN", amount: "640000", transactionAt: `${start}T09:00:00+07:00` },
      { id: "tx-early-2", type: "EXPENSE", merchant: "IndiHome", amount: "480000", transactionAt: `${start}T10:00:00+07:00` },
    ],
  );
  const earlyGroceries = emptyChange(
    "11111111-1111-4111-8111-111111111111",
    "Belanja rumah",
    "410000",
    "0.2426",
    2,
    [{ id: "22222222-2222-4222-8222-222222222222", name: "Pasar Keluarga", amount: "410000", count: 2, previous: null, deltaVsPrevious: null }],
    [{ id: "tx-early-3", type: "EXPENSE", merchant: "Pasar Keluarga", amount: "410000", transactionAt: `${start}T11:00:00+07:00` }],
  );
  const earlyDining = emptyChange(
    "55555555-5555-4555-8555-555555555555",
    "Makan di luar",
    "160000",
    "0.0947",
    1,
    [{ id: "m-kopi", name: "Warung Kopi", amount: "160000", count: 1, previous: null, deltaVsPrevious: null }],
    [{ id: "tx-early-4", type: "EXPENSE", merchant: "Warung Kopi", amount: "160000", transactionAt: "2026-09-06T09:00:00+07:00" }],
  );
  facts.categoryChanges = [earlyBills, earlyGroceries, earlyDining];
  facts.merchantDrivers = [];
  facts.history = facts.history.map((item, index) => (index === facts.history.length - 1 ? { ...item, start, end: "2026-10-05", measuredUntil: "2026-09-07", days: 2, averageDailyExpenseRounded: "845000", expense: "1690000", grossExpense: "1690000", netCashflow: "10310000" } : item));
  const earlyById = new Map(facts.categoryChanges.map(item => [item.id, item]));
  facts.categoryHistory.cycleStarts = facts.history.map(item => item.start);
  facts.categoryHistory.totals = facts.history.map(item => item.expense);
  facts.categoryHistory.rows = facts.categoryHistory.rows.map(row => {
    const updated = earlyById.get(row.id);
    const amounts = [...row.amounts.slice(0, -1), updated ? updated.amount : "0"];
    return { ...row, name: updated ? updated.name : row.name, amounts };
  });
  facts.categoryHistory.other = { ...facts.categoryHistory.other, amounts: [...facts.categoryHistory.other.amounts.slice(0, -1), "0"] };
  if (facts.categoryHistory.sameDay) {
    facts.categoryHistory.sameDay = { ...facts.categoryHistory.sameDay, elapsedDays: 2, cycleStarts: facts.categoryHistory.cycleStarts };
  }
  facts.categoryRanking = rankingFor(facts);
  facts.merchantRanking = {
    mode: "ELAPSED_DAYS",
    rows: [
      { id: "m-pln", name: "PLN", kind: "MERCHANT", amount: "640000", count: 1, previous: null, deltaVsPrevious: null, shareOfExpense: "0.3787" },
      { id: "m-indihome", name: "IndiHome", kind: "MERCHANT", amount: "480000", count: 1, previous: null, deltaVsPrevious: null, shareOfExpense: "0.2840" },
      { id: "22222222-2222-4222-8222-222222222222", name: "Pasar Keluarga", kind: "MERCHANT", amount: "410000", count: 2, previous: null, deltaVsPrevious: null, shareOfExpense: "0.2426" },
      { id: "m-kopi", name: "Warung Kopi", kind: "MERCHANT", amount: "160000", count: 1, previous: null, deltaVsPrevious: null, shareOfExpense: "0.0947" },
    ],
    other: { amount: "0", count: 0, merchantCount: 0, previous: null, deltaVsPrevious: null },
    total: { amount: "1690000", count: 5, previous: null, deltaVsPrevious: null },
  };
  return facts;
}

export const cycleCommentary = {
  id: "synthetic-commentary", status: "SUCCEEDED", historical: false, promptVersion: "cycle-analyst-v6", createdAt: "2026-09-06T11:59:59+07:00",
  text: "Belanja rumah menyumbang setengah selisih pengeluaran dari siklus sebelumnya. Makan di luar lebih tinggi dari siklus sebelumnya, tetapi dekat dengan median tiga siklus. Bukti merchant dan transaksi dapat diperiksa sebelum dibahas bersama.",
  metrics: { period_kind: "CURRENT_CYCLE", period_start: "2026-09-01", period_end: "2026-09-07" }, completedAt: "2026-09-06T12:00:00+07:00",
};

export function firstCycleFacts(start = "2026-09-01") {
  const facts = cycleFacts(start);
  facts.cycles = [facts.period];
  facts.history = [facts.history.at(-1)];
  facts.previousCycle = null;
  facts.comparison = { mode: "ELAPSED_DAYS", previous: null, previousFullCycle: null, eligibleCycles: 0, median3Available: false, expense: { amount: facts.cashflow.expense, previous: null, deltaVsPrevious: null, relativeDeltaVsPrevious: null, previousFullCycle: null, deltaVsPreviousFullCycle: null, relativeDeltaVsPreviousFullCycle: null, median3: null, deltaVsMedian3: null, relativeDeltaVsMedian3: null } };
  facts.pace = { previousFullCycle: null, median3: null };
  facts.categoryChanges = facts.categoryChanges.map(item => ({ ...item, median3: null, deltaVsMedian3: null, relativeDeltaVsMedian3: null }));
  facts.categoryHistory = { cycleStarts: facts.categoryHistory.cycleStarts.slice(-1), rows: facts.categoryHistory.rows.map(row => ({ ...row, amounts: row.amounts.slice(-1) })), other: { amounts: facts.categoryHistory.other.amounts.slice(-1) } };
  facts.takeaways = { ...facts.takeaways, verdict: "" };
  return facts;
}

export function noWealthFacts() {
  const facts = cycleFacts();
  facts.wealth = { previous: null, current: null, netWorthChange: null, confirmedCashflow: null, valuationAndOtherChange: null };
  facts.dataQuality = [{ kind: "WEALTH_SNAPSHOT_BEFORE_CYCLE", count: 1, impact: "ANALYSIS_PARTIAL", action: "/wealth" }];
  return facts;
}

export function noSpendingFacts() {
  const facts = cycleFacts();
  facts.daily = facts.daily.map(day => ({ ...day, expense: "0", grossExpense: "0", refund: "0", cumulativeExpense: "0" }));
  facts.cashflow = { income: "0", grossExpense: "0", refund: "0", expense: "0", netCashflow: "0", savingsAllocated: "0", unallocatedSurplus: "0" };
  facts.spendingShape = { days: 6, averageDailyExpense: null, averageDailyExpenseRounded: null, peakDay: null, peakExpense: null, peakShareOfExpense: null, zeroSpendDays: 6 };
  facts.comparison.expense = { ...facts.comparison.expense, amount: "0", deltaVsPrevious: "-2800000", relativeDeltaVsPrevious: null };
  facts.categoryChanges = [];
  facts.merchantDrivers = [];
  facts.savingsDestinations = [];
  facts.categoryRanking = { baseline: "SAME_DAY", rows: [], other: { name: "Kategori lain", amount: "0", count: 0, categoryCount: 0, rows: [] } };
  facts.merchantRanking = merchantRankingFor(facts);
  facts.pace = { previousFullCycle: null, median3: null };
  facts.wealth = { previous: null, current: null, netWorthChange: null, confirmedCashflow: null, valuationAndOtherChange: null };
  return facts;
}

export function fullCycleFacts(start = "2026-09-01") {
  const facts = cycleFacts(start);
  const days = 30;
  const daily = Array.from({ length: days }, (_, index) => {
    const date = new Date(`${facts.period.start}T00:00:00Z`);
    date.setUTCDate(date.getUTCDate() + index);
    return { period: date.toISOString().slice(0, 10), expense: "100000", grossExpense: "100000", refund: "0", income: index === 0 ? "12000000" : "0" };
  });
  let running = 0n;
  for (const row of daily) { running += BigInt(row.expense); row.cumulativeExpense = String(running); }
  facts.daily = daily;
  facts.period = { ...facts.period, measuredUntil: daily.at(-1).period, state: "ACTIVE", end: null };
  facts.cashflow = { ...facts.cashflow, grossExpense: "3000000", expense: "3000000", netCashflow: "9000000" };
  facts.comparison.expense = { ...facts.comparison.expense, amount: "3000000" };
  facts.spendingShape = { ...facts.spendingShape, days, averageDailyExpense: "100000.00", averageDailyExpenseRounded: "100000", peakDay: daily[0].period, peakExpense: "100000", peakShareOfExpense: "0.0333", zeroSpendDays: 0 };
  facts.pace = {
    previousFullCycle: Array.from({ length: days }, (_, index) => String(Math.round((index + 1) * 2800000 / days))),
    median3: Array.from({ length: days }, (_, index) => String(Math.round((index + 1) * 3800000 / days))),
  };
  const last = facts.history.at(-1);
  Object.assign(last, { days, averageDailyExpenseRounded: "100000", income: "12000000", grossExpense: "3000000", expense: "3000000", netCashflow: "9000000" });
  return facts;
}

export function stableCycleFacts() {
  const facts = cycleFacts("2026-08-26");
  const category = { ...facts.categoryChanges[0], amount: "1400000", previous: "1400000", previousFullCycle: "1400000", deltaVsPreviousFullCycle: "0", relativeDeltaVsPreviousFullCycle: "0", median3: "1400000", deltaVsPrevious: "0", deltaVsMedian3: "0", relativeDeltaVsPrevious: "0", relativeDeltaVsMedian3: "0", shareOfExpense: "1", contributionToExpenseChange: null, count: 6, merchants: [], transactions: [] };
  facts.categoryChanges = [category];
  facts.comparison.expense = { ...category, id: "", name: "" };
  facts.cashflow = { income: "1400000", grossExpense: "1400000", refund: "0", expense: "1400000", netCashflow: "0", savingsAllocated: "0", unallocatedSurplus: "0" };
  facts.daily = facts.daily.map((day, index) => ({ ...day, expense: index < 4 ? "200000" : "300000", grossExpense: index < 4 ? "200000" : "300000", refund: "0", income: index === 0 ? "1400000" : "0" }));
  let stableTotal = 0n;
  for (const day of facts.daily) { stableTotal += BigInt(day.expense); day.cumulativeExpense = String(stableTotal); }
  const last = facts.history.at(-1);
  Object.assign(last, { income: "1400000", grossExpense: "1400000", expense: "1400000", netCashflow: "0", days: 6, averageDailyExpenseRounded: "233333" });
  last.expenseDelta = String(BigInt(last.expense) - BigInt(facts.history.at(-2).expense));
  facts.categoryHistory = { cycleStarts: facts.history.map(item => item.start), rows: [{ id: category.id, name: category.name, amounts: facts.history.map(item => item.expense) }], other: { amounts: facts.history.map(() => "0") }, totals: facts.history.map(item => item.expense) };
  facts.spendingShape = { days: 6, averageDailyExpense: "233333.33", averageDailyExpenseRounded: "233333", peakDay: facts.daily[4].period, peakExpense: "300000", peakShareOfExpense: "0.2143", zeroSpendDays: 0 };
  facts.merchantDrivers = []; facts.memberAttribution = [{ id: "", name: "Shared / unattributed", amount: "1400000", count: 6, kind: "AUTOMATIC", shareOfExpense: "1.0000" }]; facts.savingsDestinations = [];
  facts.merchantRanking = merchantRankingFor(facts);
  facts.wealth.current.netWorth = facts.wealth.previous.netWorth;
  facts.wealth.netWorthChange = "0"; facts.wealth.confirmedCashflow = "0"; facts.wealth.valuationAndOtherChange = "0";
  facts.dataQuality = [];
  facts.takeaways = { ...facts.takeaways, verdict: "Sama dengan siklus lalu (penuh)." };
  facts.categoryRanking = rankingFor(facts);
  return facts;
}
