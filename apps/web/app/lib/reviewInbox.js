
export function legacyViewTarget(view) {
  if (view === "actions") return "/household?tab=sumber#tindakan";
  if (view === "email") return "/household?tab=sumber#email-masuk-terakhir";
  return null;
}

export function inboxView(search) {
  const view = new URLSearchParams(search || "").get("view");
  return view === "history" ? "history" : "transactions";
}

export function categoryAnswers(item, categories = []) {
  const active = new Map(categories.filter(category => category.active !== false).map(category => [category.id, category]));
  const answers = (item?.suggestedCategories || []).filter(value => active.size === 0 || active.has(value.id)).slice(0, 3)
    .map((value, index) => ({ id: value.id, name: value.name, often: index === 0 && value.reason === "MERCHANT_HISTORY" }));
  const shown = new Set(answers.map(value => value.id));
  const others = [...active.values()].filter(category => !shown.has(category.id)).map(category => ({ id: category.id, name: category.name }));
  return { answers, others };
}

export function rememberLabel(item) {
  return item?.merchantName || item?.counterparty || "";
}

const transferLabels = { OWN_ACCOUNT: "rekening sendiri", HOUSEHOLD_ACCOUNT: "rekening rumah tangga", INVESTMENT_ACCOUNT: "investasi", ASSET_PURCHASE: "beli aset", EXPENSE: "pengeluaran", IGNORE: "diabaikan" };
const resolveLabels = { IGNORE: "diabaikan", MERGE_EXISTING: "disatukan", CONFIRM_NEW_TRANSFER: "transfer baru dicatat", SET_WEALTH_ACCOUNT: "akun dipilih", SET_FINANCIAL_EMAIL_ENTITIES: "rekening dipilih", ALLOCATE_RETAINED_BALANCE: "sisa dialokasikan", LEAVE_UNALLOCATED: "dibiarkan", COMPLETE_BANK_FACTS: "dilengkapi", SET_PAY_DATE: "tanggal diisi", PRIMARY_SALARY: "gaji utama", HOUSEHOLD_POLICY: "dicatat sebagai gaji", ORDINARY_INCOME: "pemasukan biasa", ACKNOWLEDGE: "dimengerti" };

export function resolutionSummary(name, body, categories = []) {
  if (name === "confirm") {
    const category = categories.find(value => value.id === body?.categoryId);
    return category ? `→ ${category.name}` : "disimpan";
  }
  if (name === "reject") return "diabaikan";
  if (name === "merge") return "disatukan";
  if (name === "classify-transfer") return transferLabels[body?.classification] || "dikelompokkan";
  return resolveLabels[body?.action] || "selesai";
}

const UNDOABLE_RESOLVE = new Set(["IGNORE", "LEAVE_UNALLOCATED", "SET_WEALTH_ACCOUNT", "SET_FINANCIAL_EMAIL_ENTITIES"]);
const UNDOABLE_TYPES = new Set(["EXPENSE", "INCOME", "REFUND"]);

export function canUndoResolution(item, name, body) {
  if (item?.presentation === "NOTICE") return false;
  if (item?.subjectType) return name === "resolve" && UNDOABLE_RESOLVE.has(body?.action);
  return name === "confirm" && Boolean(item?.reviewItemId) && UNDOABLE_TYPES.has(item?.type);
}

export function resolvedLine(item, name, body, categories = []) {
  return {
    id: item.id,
    reopenId: item.reviewItemId || item.id,
    transactionId: item.subjectType ? null : item.id,
    canUndo: canUndoResolution(item, name, body),
    label: item.merchantName || item.counterparty || item.description || "Transaksi",
    amount: item.amount || null,
    summary: resolutionSummary(name, body, categories),
  };
}
