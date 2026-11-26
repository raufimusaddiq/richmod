import { cycleFacts } from "../../tests/fixtures/cycle-review.mjs";

export const layoutCategories = () => Array.from({ length: 14 }, (_, index) => ({
  id: `cat-${index + 1}`,
  parentId: index === 7 ? "cat-1" : (index === 1 || index === 4 || index === 10) ? "cat-10" : null,
  name: index === 0 ? "Belanja Bahan Makanan & Kebutuhan Rumah Tangga" : `Kategori Keluarga ${index + 1}`,
  amount: String((14 - index) * 120000),
  active: true,
  slug: `kategori-${index + 1}`,
}));

const accounts = () => [
  { id: "acc-1", name: "Rekening Bank Jago Utama Keluarga", accountType: "BANK", trackingPolicy: "SPENDING_ONLY", active: true },
  { id: "acc-2", name: "BCA Tabungan Bersama", accountType: "BANK", trackingPolicy: "FULL_LEDGER", active: true },
];

const members = () => [
  { id: "member-1", displayName: "Rafi Musaddiq", email: "rafi@example.test", role: "OWNER", active: true, telegramConnected: true, passwordInitialized: true },
  { id: "member-2", displayName: "Dina Rahmawati", email: "dina@example.test", role: "MEMBER", active: true, telegramConnected: false, passwordInitialized: true },
];

function transactions(categories, accountList, memberList) {
  return Array.from({ length: 24 }, (_, index) => ({
    id: `tx-${index + 1}`,
    type: index % 5 === 0 ? "INCOME" : "EXPENSE",
    status: "CONFIRMED",
    amount: String(45000 + index * 15000),
    merchantName: `Merchant Contoh Panjang ${index + 1}`,
    description: `Transaksi harian rumah tangga nomor ${index + 1}`,
    categoryId: categories[index % categories.length].id,
    categoryName: categories[index % categories.length].name,
    accountId: accountList[index % accountList.length].id,
    accountName: accountList[index % accountList.length].name,
    memberName: memberList[index % memberList.length].displayName,
    sourceType: "BANK_EMAIL",
    transactionAt: `2026-09-${String(28 - (index % 20)).padStart(2, "0")}T09:20:00+07:00`,
    updatedAt: "2026-09-28T10:00:00Z",
  }));
}

const daily = () => Array.from({ length: 12 }, (_, index) => ({
  period: `2026-09-${String(index + 1).padStart(2, "0")}`,
  grossExpense: String((index % 4) * 80000 + 120000),
  refund: index === 2 ? "15000" : "0",
  expense: String((index % 4) * 80000 + 120000),
  income: index === 0 ? "12500000" : "0",
}));

const wealthAccounts = () => [
  { id: "wa-1", name: "Tabungan utama", institution: "Bank Jago", wealthType: "BANK", usageRole: "SAVINGS", active: true },
  { id: "wa-2", name: "Reksa dana pasar uang", institution: "Bibit", wealthType: "MUTUAL_FUND", usageRole: "INVESTMENT", active: true },
];

const wealthItems = () => [
  { wealthAccountId: "wa-1", name: "Tabungan utama", side: "ASSET", valueIdr: "30000000" },
  { wealthAccountId: "wa-2", name: "Reksa dana pasar uang", side: "ASSET", valueIdr: "20000000" },
];

const wealthSnapshots = () => [
  { id: "wealth-2", observedAt: "2026-09-09T15:00:00+07:00", netWorthIdr: "50000000", assetTotalIdr: "50000000", liabilityTotalIdr: "0", items: wealthItems() },
  { id: "wealth-1", observedAt: "2026-09-08T10:00:00+07:00", netWorthIdr: "48000000", assetTotalIdr: "48000000", liabilityTotalIdr: "0", items: wealthItems() },
];

function alignedCycleFacts() {
  const facts = cycleFacts();
  const idMap = new Map([
    ["11111111-1111-4111-8111-111111111111", "cat-1"],
    ["55555555-5555-4555-8555-555555555555", "cat-2"],
    ["66666666-6666-4666-8666-666666666666", "cat-3"],
  ]);
  const remap = row => (row?.id && idMap.has(row.id) ? { ...row, id: idMap.get(row.id) } : row);
  const travel = {
    id: "cat-8",
    name: "Perjalanan",
    amount: "631000",
    previous: "0",
    median3: "0",
    deltaVsPrevious: "631000",
    deltaVsMedian3: "631000",
    relativeDeltaVsPrevious: null,
    relativeDeltaVsMedian3: null,
    shareOfExpense: "0.1306",
    contributionToExpenseChange: "0.3100",
    count: 2,
    previousFullCycle: "0",
    deltaVsPreviousFullCycle: "631000",
    relativeDeltaVsPreviousFullCycle: null,
    merchants: [],
    transactions: [],
  };
  facts.categoryChanges = [...facts.categoryChanges.map(remap), travel];
  facts.categoryRanking = {
    ...facts.categoryRanking,
    rows: facts.categoryRanking.rows.map(remap),
    other: {
      ...facts.categoryRanking.other,
      amount: "631000",
      count: 2,
      categoryCount: 1,
      shareOfExpense: "0.1306",
      rows: [{ id: "cat-8", name: "Perjalanan", amount: "631000", count: 2, shareOfExpense: "0.1306" }],
    },
  };
  facts.categoryHistory = {
    ...facts.categoryHistory,
    rows: facts.categoryHistory.rows.map(remap),
    sameDay: facts.categoryHistory.sameDay ? { ...facts.categoryHistory.sameDay, rows: facts.categoryHistory.sameDay.rows.map(remap) } : null,
  };
  return facts;
}

export function layoutApi(api) {
  const categories = layoutCategories();
  const accountList = accounts();
  const memberList = members();
  const transactionList = transactions(categories, accountList, memberList);
  const snapshots = wealthSnapshots();
  const dailySeries = daily();

  api.use(({ path }) => {
    if (path === "/api/v1/analytics/cycle-review") return alignedCycleFacts();
    if (path === "/api/v1/analytics/cycle-decisions") return { items: [], previous: [], previousCycleStart: "2026-08-26" };
    if (path === "/api/v1/analytics/overview") return { income: "12500000", expense: "2590000", netCashflow: "9910000", savingsAllocated: "6500000", unallocatedSurplus: "3410000", reviewCount: 1, periodKind: "CURRENT_CYCLE" };
    if (path === "/api/v1/analytics/cycle" || path === "/api/v1/analytics/cycle/daily") return { kind: "CURRENT_CYCLE", start: "2026-09-01", end: "2026-09-30", cycleStart: "2026-09-01", daysElapsed: 12, daysTotal: 30, daily: dailySeries };
    if (path.startsWith("/api/v1/analytics/categories")) return categories;
    if (path.startsWith("/api/v1/analytics/cashflow")) return [{ period: "2026-08", income: "12500000", expense: "8100000", refund: "0", netCashflow: "4400000" }, { period: "2026-09", income: "12500000", expense: "2590000", refund: "0", netCashflow: "9910000" }];
    if (path.startsWith("/api/v1/analytics/merchants")) return [{ name: "Pasar Minggu", amount: "1250000" }];
    if (path.startsWith("/api/v1/analytics/members")) return [{ name: "Rafi", amount: "2590000" }];
    if (path === "/api/v1/insights") return [];
    if (path === "/api/v1/wealth/snapshots/latest") return snapshots[0];
    if (path === "/api/v1/wealth/accounts") return wealthAccounts();
    if (path === "/api/v1/wealth/history") return snapshots;
    if (path === "/api/v1/wealth/summary") return { latest: snapshots[0], previous: snapshots[1], netWorthChangeIdr: "2000000", confirmedCashflowIdr: "1500000", valuationAndOtherChangeIdr: "500000" };
    if (path === "/api/v1/wealth/current-cycle-savings") return null;
    if (path === "/api/v1/wealth/cycle-recaps" || path === "/api/v1/wealth/observations") return [];
    if (path === "/api/v1/transactions/quick-suggestions") {
      return {
        frequent: [
          { description: "Kopi susu", amount: "25000", type: "EXPENSE", categoryId: "cat-1", accountId: "acc-1" },
          { description: "Makan siang", amount: "45000", type: "EXPENSE", categoryId: "cat-2", accountId: "acc-1" },
          { description: "Parkir harian", amount: "10000", type: "EXPENSE", categoryId: "cat-3", accountId: "acc-1" },
          { description: "Belanja pasar", amount: "120000", type: "EXPENSE", categoryId: "cat-1", accountId: "acc-2" },
        ],
        recentCategories: ["cat-1", "cat-2", "cat-3"],
        descriptionMap: {},
      };
    }
    if (/\/api\/v1\/transactions\/[^/]+\/evidence$/.test(path)) return [{ id: "ev-1", evidenceType: "BANK_EMAIL", sourceType: "BANK_EMAIL", receivedAt: "2026-09-28T09:20:00+07:00" }];
    if (/\/api\/v1\/transactions\/[^/]+\/audit$/.test(path)) return [{ id: "audit-1", action: "CONFIRM", actorType: "SYSTEM", createdAt: "2026-09-28T09:21:00+07:00" }];
    if (/\/api\/v1\/transactions\/[^/]+$/.test(path)) return transactionList[0];
    if (path.startsWith("/api/v1/transactions")) return transactionList;
    if (path === "/api/v1/reviews") return [{ id: "review-1", reason: "AMBIGUOUS_CATEGORY", amount: "750000", merchantName: "Transfer ke rekening lain", transactionAt: "2026-09-05T15:00:00+07:00", sourceType: "BANK_EMAIL", proposalStatus: "NEEDS_REVIEW", missingFacts: ["merchant"], description: "Tujuan transfer belum jelas" }];
    if (path === "/api/v1/integration-actions") return [];
    if (path === "/api/v1/categories") return categories;
    if (path === "/api/v1/accounts") return accountList;
    if (path === "/api/v1/household") return { id: "household-1", name: "Rumah Rafi" };
    if (path === "/api/v1/household/members") return memberList;
    if (path === "/api/v1/documents") return [{ id: "doc-1", status: "SUCCEEDED", documentType: "RECEIPT", sourceType: "WEB_IMAGE", createdAt: "2026-09-06T10:00:00+07:00", confidence: 0.93, linkedTransactionIds: ["tx-1"], summary: { merchant: "Pasar Minggu", amount: "185000" }, needsReview: false }];
    if (path === "/api/v1/merchant-aliases") return [{ id: "alias-1", normalizedName: "Pasar Minggu", rawName: "PASAR MINGGU", defaultCategoryName: "Belanja", autoApply: true }];
    if (path === "/api/v1/known-accounts") return [{ id: "known-1", institution: "Bank", displayName: "Rekening keluarga", matchHint: "1234", relationship: "HOUSEHOLD_ACCOUNT" }];
    if (path === "/api/v1/operations/status") return { status: "HEALTHY", checkedAt: "2026-09-07T10:00:00+07:00", jobs: { pending: 0, failed: 0 }, reviewBacklog: 1, worker: { healthy: true }, llmGateway: { configured: true } };
    if (path === "/api/v1/salary/sources") return [{ id: "salary-1", employer: "Richmod Labs", active: true, isPrimary: true }];
    if (path === "/api/v1/bank-email-listeners" || path === "/api/v1/financial-email-sources") return [];
    if (path === "/api/v1/integrations/email-ingress") return { address: "household@example.richmod.link", status: "ACTIVE", lastReceivedAt: "2026-09-06T09:20:00+07:00" };
    if (path === "/api/v1/operations/interaction-weekly") return { windowDays: 7, timezone: "Asia/Jakarta", current: { start: "2026-09-01T00:00:00+07:00", end: "2026-09-08T00:00:00+07:00", cardsSent: 160, finishedTelegram: 111, finishedWeb: 30, stillOpen: 19, finishedTelegramRate: 0.69375, typesSent: 2, typesMostlyTelegram: 1, types: [{ reviewType: "UNKNOWN_MERCHANT", sent: 100, finishedTelegram: 90, finishedWeb: 5, stillOpen: 5 }, { reviewType: "TRANSFER_CLASSIFICATION", sent: 60, finishedTelegram: 21, finishedWeb: 25, stillOpen: 14 }], answers: 200, unusableAnswers: 4, unusableAnswerRate: 0.0196, answerSources: { TAP: 120, REPLY: 40, THREAD_REPLY: 10, PLAIN: 25, LABEL_TEXT: 5, VIA_ASSISTANT: 0 }, repeatedInput: 1, brokenReplies: 0, mechanicsReplies: 0, ubahReopened: 3, medianFinishMs: 48000 }, previous: { start: "2026-08-25T00:00:00+07:00", end: "2026-09-01T00:00:00+07:00", cardsSent: 160, finishedTelegram: 111, finishedWeb: 30, stillOpen: 19, finishedTelegramRate: 0.69375, typesSent: 2, typesMostlyTelegram: 1, types: [{ reviewType: "UNKNOWN_MERCHANT", sent: 100, finishedTelegram: 90, finishedWeb: 5, stillOpen: 5 }, { reviewType: "TRANSFER_CLASSIFICATION", sent: 60, finishedTelegram: 21, finishedWeb: 25, stillOpen: 14 }], answers: 200, unusableAnswers: 4, unusableAnswerRate: 0.0196, answerSources: { TAP: 120, REPLY: 40, THREAD_REPLY: 10, PLAIN: 25, LABEL_TEXT: 5, VIA_ASSISTANT: 0 }, repeatedInput: 1, brokenReplies: 0, mechanicsReplies: 0, ubahReopened: 3, medianFinishMs: 48000 }, notYetMeasurable: [] };
    if (path === "/api/v1/admin/overview") return { status: "HEALTHY", checkedAt: "2026-09-07T10:00:00Z", worker: { healthy: true, lastHeartbeatAt: "2026-09-07T10:00:00Z" }, jobs: { pending: 0, running: 0, failed24h: 0, lanes: ["INTERACTIVE", "CHAT", "DEFAULT", "BACKGROUND"].map(lane => ({ lane, pending: 0, running: 0, oldestDueAgeMs: null })) }, llm: { calls24h: 12, failed24h: 0, successRate: 1, p95DurationMs: 820 }, reviews: { open: 1 }, households: { total: 1 }, integrations: { llmGatewayConfigured: true, llmProtocol: "Cloud gateway" }, recentEvents: [] };
    if (path.startsWith("/api/v1/admin/")) return { items: [], nextCursor: null };
    return undefined;
  });
}
