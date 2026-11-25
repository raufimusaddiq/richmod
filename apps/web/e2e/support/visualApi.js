import { reply } from "./api.js";
import { superAdmin } from "./data.js";
import { cycleFacts, firstCycleFacts, fullCycleFacts, noSpendingFacts, noWealthFacts } from "../../tests/fixtures/cycle-review.mjs";

export const visualUser = superAdmin;

const transactions = () => [
  { id: "tx-1", type: "EXPENSE", status: "CONFIRMED", amount: "185000", merchantName: "Pasar Minggu", categoryName: "Belanja rumah", memberName: "Rafi", sourceType: "BANK_EMAIL", transactionAt: "2026-09-06T09:20:00+07:00", accountName: "Jago utama" },
  { id: "tx-2", type: "INCOME", status: "CONFIRMED", amount: "12500000", merchantName: "Gaji September", categoryName: "Pendapatan", memberName: "Rafi", sourceType: "BANK_EMAIL", transactionAt: "2026-09-01T08:00:00+07:00", accountName: "Jago utama" },
  { id: "tx-3", type: "TRANSFER", status: "NEEDS_REVIEW", amount: "750000", counterpartyName: "Transfer ke rekening lain", categoryName: null, memberName: "Rafi", sourceType: "BANK_EMAIL", transactionAt: "2026-09-05T15:00:00+07:00", accountName: "Jago utama" },
];

const categories = () => [
  { id: "cat-1", name: "Belanja rumah", amount: "1250000", active: true, slug: "belanja-rumah" },
  { id: "cat-2", name: "Transportasi", amount: "640000", active: true, slug: "transportasi" },
  { id: "cat-3", name: "Makan", amount: "420000", active: true, slug: "makan" },
  { id: "cat-4", name: "Tagihan", amount: "280000", active: true, slug: "tagihan" },
];

const calendarCategories = () => [
  "Belanja rumah", "Transportasi", "Makan", "Tagihan", "Kesehatan", "Pendidikan", "Hiburan", "Hadiah",
  "Perawatan diri", "Perlengkapan", "Donasi", "Biaya admin", "Lain-lain",
].map((name, index) => ({ id: `cal-cat-${index + 1}`, name, amount: String((13 - index) * 180000) }));

const daily = () => Array.from({ length: 12 }, (_, index) => ({ period: `2026-09-${String(index + 1).padStart(2, "0")}`, grossExpense: String((index % 4) * 80000 + 120000), refund: "0", expense: String((index % 4) * 80000 + 120000), income: index === 0 ? "12500000" : "0" }));

const review = () => [{ id: "review-1", reason: "AMBIGUOUS_CATEGORY", amount: "750000", merchantName: "Transfer ke rekening lain", transactionAt: "2026-09-05T15:00:00+07:00", sourceType: "BANK_EMAIL", proposalStatus: "NEEDS_REVIEW", missingFacts: ["merchant"], description: "Tujuan transfer belum jelas" }];

const members = () => [
  { id: "member-1", displayName: "Rafi", email: "rafi@example.test", role: "OWNER", active: true, telegramConnected: true },
  { id: "member-2", displayName: "Dina", email: "dina@example.test", role: "MEMBER", active: true, telegramConnected: false },
];

const document = () => ({ id: "doc-1", status: "SUCCEEDED", documentType: "RECEIPT", sourceType: "WEB_IMAGE", createdAt: "2026-09-06T10:00:00+07:00", confidence: 0.93, linkedTransactionIds: ["tx-1"], summary: { merchant: "Pasar Minggu", amount: "185000" }, needsReview: false });

const wealthAccounts = () => [
  { id: "wa-1", name: "Tabungan utama", institution: "Bank Jago", wealthType: "BANK", usageRole: "SPENDING", active: true },
  { id: "wa-2", name: "Reksa dana pasar uang", institution: "Bibit", wealthType: "MUTUAL_FUND", usageRole: "SAVINGS", active: true },
  { id: "wa-3", name: "Kartu kredit", institution: "BCA", wealthType: "LOAN", usageRole: "LIABILITY", active: true },
];

const wealthItems = () => [
  { wealthAccountId: "wa-1", name: "Tabungan utama", side: "ASSET", valueIdr: "30000000" },
  { wealthAccountId: "wa-2", name: "Reksa dana pasar uang", side: "ASSET", valueIdr: "20000000" },
  { wealthAccountId: "wa-3", name: "Kartu kredit", side: "LIABILITY", valueIdr: "1750000" },
];

const wealthSnapshots = () => [
  { id: "wealth-3", observedAt: "2026-09-09T15:00:00+07:00", netWorthIdr: "48250000", assetTotalIdr: "50000000", liabilityTotalIdr: "1750000", items: wealthItems() },
  { id: "wealth-2", observedAt: "2026-09-09T09:00:00+07:00", netWorthIdr: "47000000", assetTotalIdr: "48750000", liabilityTotalIdr: "1750000", items: wealthItems() },
  { id: "wealth-1", observedAt: "2026-09-08T10:00:00+07:00", netWorthIdr: "46000000", assetTotalIdr: "47750000", liabilityTotalIdr: "1750000", items: wealthItems() },
];

function cycleReviewFacts(start, scenario) {
  const facts = scenario.firstCycle ? firstCycleFacts(start) : scenario.fullCycle ? fullCycleFacts(start) : scenario.noWealth ? noWealthFacts() : scenario.noSpending ? noSpendingFacts() : cycleFacts(start);
  if (scenario.fewCycles) {
    facts.history = facts.history.slice(-2);
    const rows = facts.categoryHistory;
    facts.categoryHistory = { cycleStarts: rows.cycleStarts.slice(-2), rows: rows.rows.map(row => ({ ...row, amounts: row.amounts.slice(-2), deltaVsPrevious: null })), other: { ...rows.other, amounts: rows.other.amounts.slice(-2), deltaVsPrevious: null }, totals: rows.totals.slice(-2), totalDeltaVsPrevious: null, sameDay: null };
  }
  return facts;
}

export function visualApi(api, { scenario = {}, authenticated = true } = {}) {
  const transactionList = transactions();
  const categoryList = categories();
  const calendarList = calendarCategories();
  const dailySeries = daily();
  const snapshots = wealthSnapshots();

  api.use(({ path, query }) => {
    if (!authenticated && path === "/api/v1/auth/me") return reply({}, { status: 401 });
    if (path === "/api/v1/analytics/cycle-review") return cycleReviewFacts(query.get("cycle_start") || undefined, scenario);
    if (path === "/api/v1/analytics/cycle-decisions") return { items: [], previous: [], previousCycleStart: "2026-08-26" };
    if (path === "/api/v1/analytics/overview") return { income: "12500000", expense: "2590000", netCashflow: "9910000", savingsAllocated: "6500000", unallocatedSurplus: "3410000", reviewCount: 1, periodKind: "CURRENT_CYCLE" };
    if (path === "/api/v1/wealth/snapshots/latest") return snapshots[0];
    if (path === "/api/v1/wealth/accounts") return wealthAccounts();
    if (path === "/api/v1/wealth/history") return snapshots;
    if (path === "/api/v1/wealth/summary") return { latest: snapshots[0], previous: snapshots[1], netWorthChangeIdr: "2250000", confirmedCashflowIdr: "1500000", valuationAndOtherChangeIdr: "750000" };
    if (path === "/api/v1/wealth/current-cycle-savings") return null;
    if (path === "/api/v1/wealth/cycle-recaps") return [];
    if (path === "/api/v1/wealth/observations") return [];
    if (path === "/api/v1/analytics/cycle" || path === "/api/v1/analytics/cycle/daily") return { kind: "CURRENT_CYCLE", start: "2026-09-01", end: "2026-09-30", cycleStart: "2026-09-01", salary: "12500000", spent: "2590000", remaining: "9910000", daysElapsed: 6, daysTotal: 30, daily: dailySeries };
    if (path.startsWith("/api/v1/analytics/categories")) return query.get("period") === "last_3_cycles" ? categoryList : calendarList;
    if (path.startsWith("/api/v1/analytics/cashflow")) return [{ period: "2026-06", income: "0", expense: "0", refund: "0", netCashflow: "0" }, { period: "2026-07", income: "11800000", expense: "7200000", refund: "0", netCashflow: "4600000" }, { period: "2026-08", income: "12500000", expense: "8100000", refund: "0", netCashflow: "4400000" }, { period: "2026-09", income: "12500000", expense: "2590000", refund: "0", netCashflow: "9910000" }, { period: "2026-10", income: "0", expense: "0", refund: "0", netCashflow: "0" }, { period: "2026-11", income: "0", expense: "0", refund: "0", netCashflow: "0" }];
    if (path.startsWith("/api/v1/analytics/merchants")) return [{ name: "Pasar Minggu", amount: "1250000" }, { name: "Grab", amount: "640000" }];
    if (path.startsWith("/api/v1/analytics/members")) return [{ name: "Rafi", amount: "2590000" }];
    if (path === "/api/v1/insights") return [];
    if (/\/api\/v1\/transactions\/[^/]+\/evidence$/.test(path)) return [{ id: "ev-1", evidenceType: "BANK_EMAIL", sourceType: "BANK_EMAIL", receivedAt: "2026-09-06T09:20:00+07:00" }];
    if (/\/api\/v1\/transactions\/[^/]+\/audit$/.test(path)) return [{ id: "audit-1", action: "CONFIRM", actorType: "SYSTEM", createdAt: "2026-09-06T09:21:00+07:00" }];
    if (/\/api\/v1\/transactions\/[^/]+$/.test(path)) return transactionList.find(item => path.endsWith(item.id)) || transactionList[0];
    if (path.startsWith("/api/v1/transactions")) return transactionList;
    if (path === "/api/v1/reviews") return review();
    if (path === "/api/v1/integration-actions") return [];
    if (path === "/api/v1/categories") return categoryList;
    if (path === "/api/v1/accounts") return [{ id: "account-1", name: "Jago utama", accountType: "BANK", trackingPolicy: "SPENDING_ONLY", active: true }];
    if (path === "/api/v1/household") return { id: "household-1", name: "Rumah Rafi" };
    if (path === "/api/v1/household/members") return members();
    if (path === "/api/v1/documents") return [document()];
    if (/\/api\/v1\/documents\/[^/]+\/pages$/.test(path)) return [{ index: 0 }, { index: 1 }];
    if (/\/api\/v1\/documents\/[^/]+\/extraction$/.test(path)) return [{ stage: "RECEIPT", schemaVersion: 1, validated: true, output: { merchant: "Pasar Minggu", amount: "185000" } }];
    if (path === "/api/v1/merchant-aliases") return [{ id: "alias-1", normalizedName: "Pasar Minggu", rawName: "PASAR MINGGU", defaultCategoryName: "Belanja rumah", autoApply: true }];
    if (path === "/api/v1/known-accounts") return [{ id: "known-1", institution: "Bank", displayName: "Rekening keluarga", matchHint: "1234", relationship: "HOUSEHOLD" }];
    if (path === "/api/v1/operations/status") return { status: "HEALTHY", checkedAt: "2026-09-07T10:00:00+07:00", jobs: { pending: 0, failed: 0 }, reviewBacklog: 1, worker: { healthy: true }, llmGateway: { configured: true } };
    if (path === "/api/v1/salary/sources") return [{ id: "salary-1", employer: "Richmod Labs", active: true, isPrimary: true }];
    if (path === "/api/v1/bank-email-listeners") return [];
    if (path === "/api/v1/integrations/email-ingress") return { address: "household@example.richmod.link", status: "ACTIVE", lastReceivedAt: "2026-09-06T09:20:00+07:00" };
    if (path === "/api/v1/operations/interaction-weekly") return { windowDays: 7, timezone: "Asia/Jakarta", current: { start: "2026-09-01T00:00:00+07:00", end: "2026-09-08T00:00:00+07:00", cardsSent: 160, finishedTelegram: 111, finishedWeb: 30, stillOpen: 19, finishedTelegramRate: 0.69375, typesSent: 2, typesMostlyTelegram: 1, types: [{ reviewType: "UNKNOWN_MERCHANT", sent: 100, finishedTelegram: 90, finishedWeb: 5, stillOpen: 5 }, { reviewType: "TRANSFER_CLASSIFICATION", sent: 60, finishedTelegram: 21, finishedWeb: 25, stillOpen: 14 }], answers: 200, unusableAnswers: 4, unusableAnswerRate: 0.0196, answerSources: { TAP: 120, REPLY: 40, THREAD_REPLY: 10, PLAIN: 25, LABEL_TEXT: 5, VIA_ASSISTANT: 0 }, repeatedInput: 1, brokenReplies: 0, mechanicsReplies: 0, medianFinishMs: 48000 }, previous: { start: "2026-08-25T00:00:00+07:00", end: "2026-09-01T00:00:00+07:00", cardsSent: 160, finishedTelegram: 111, finishedWeb: 30, stillOpen: 19, finishedTelegramRate: 0.69375, typesSent: 2, typesMostlyTelegram: 1, types: [{ reviewType: "UNKNOWN_MERCHANT", sent: 100, finishedTelegram: 90, finishedWeb: 5, stillOpen: 5 }, { reviewType: "TRANSFER_CLASSIFICATION", sent: 60, finishedTelegram: 21, finishedWeb: 25, stillOpen: 14 }], answers: 200, unusableAnswers: 4, unusableAnswerRate: 0.0196, answerSources: { TAP: 120, REPLY: 40, THREAD_REPLY: 10, PLAIN: 25, LABEL_TEXT: 5, VIA_ASSISTANT: 0 }, repeatedInput: 1, brokenReplies: 0, mechanicsReplies: 0, medianFinishMs: 48000 }, notYetMeasurable: [] };
    if (path === "/api/v1/admin/overview") return { status: "HEALTHY", checkedAt: "2026-09-07T10:00:00Z", worker: { healthy: true, lastHeartbeatAt: "2026-09-07T10:00:00Z" }, jobs: { pending: 0, running: 0, failed24h: 0, lanes: ["INTERACTIVE", "CHAT", "DEFAULT", "BACKGROUND"].map(lane => ({ lane, pending: 0, running: 0, oldestDueAgeMs: null })) }, llm: { calls24h: 12, failed24h: 0, successRate: 1, p95DurationMs: 820 }, reviews: { open: 1 }, households: { total: 1 }, integrations: { llmGatewayConfigured: true, llmProtocol: "Cloud gateway" }, recentEvents: [] };
    if (path === "/api/v1/admin/jobs/job-1234567890abcdef") return { id: "job-1234567890abcdef", type: "SYNC", lane: "DEFAULT", status: "SUCCEEDED", attempts: 1, maxAttempts: 3, createdAt: "2026-09-07T09:57:00Z", startedAt: "2026-09-07T09:58:00Z", finishedAt: "2026-09-07T09:58:02Z", references: {}, retries: [] };
    if (path === "/api/v1/admin/jobs") return { items: [{ id: "job-1234567890abcdef", status: "SUCCEEDED", type: "SYNC", lane: "DEFAULT", attempts: 1, maxAttempts: 3, startedAt: "2026-09-07T09:58:00Z", finishedAt: "2026-09-07T09:58:02Z", updatedAt: "2026-09-07T09:58:02Z" }], nextCursor: null };
    if (path.startsWith("/api/v1/admin/")) return { items: [], nextCursor: null };
    return undefined;
  });
}
