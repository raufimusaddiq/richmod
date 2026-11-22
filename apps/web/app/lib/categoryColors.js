export const CATEGORY_COLORS = [
  "var(--chart-category-1)",
  "var(--chart-category-2)",
  "var(--chart-category-3)",
  "var(--chart-category-4)",
  "var(--chart-category-5)",
  "var(--chart-category-6)",
];

export const OTHER_CATEGORY_COLOR = "var(--chart-category-other)";

export const TOP_CATEGORY_LIMIT = CATEGORY_COLORS.length;

export function normalizeCategoryKey(value) {
  return String(value ?? "").normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLowerCase().replace(/[^a-z0-9]+/g, " ").trim();
}

export const categoryKey = item => item?.id || "uncategorized";

export function categoryColorIdentity(item) {
  if (!item) return "uncategorized";
  if (typeof item === "string") return normalizeCategoryKey(item) || "uncategorized";
  if (item.id) return String(item.id);
  if (item.id === null || item.id === "") return "uncategorized";
  return normalizeCategoryKey(item.slug || item.name) || "uncategorized";
}

function parseExactAmount(value) {
  if (typeof value === "bigint") return value * 10000n;
  const raw = String(value ?? "").trim();
  if (!raw) return 0n;
  const match = /^([+-]?)(\d+)(?:\.(\d+))?$/.exec(raw);
  if (!match) return 0n;
  const sign = match[1] === "-" ? -1n : 1n;
  const whole = BigInt(match[2]);
  const frac = (match[3] || "").slice(0, 4).padEnd(4, "0");
  return sign * (whole * 10000n + BigInt(frac));
}

function compareKeys(left, right) {
  const a = String(left);
  const b = String(right);
  return a < b ? -1 : a > b ? 1 : 0;
}

export function buildCategoryPalette(rankedCategories = []) {
  const byKey = new Map();
  for (const item of Array.isArray(rankedCategories) ? rankedCategories : []) {
    if (!item || item.other || item.remainder) continue;
    if (!item.id) continue;
    const key = String(item.id);
    if (key === "other" || key === "uncategorized") continue;
    const amount = parseExactAmount(item.amount);
    if (amount <= 0n) continue;
    const existing = byKey.get(key);
    if (!existing || amount > existing.amount) {
      byKey.set(key, { key, amount });
    }
  }
  const sorted = [...byKey.values()].sort((left, right) => {
    if (right.amount > left.amount) return 1;
    if (right.amount < left.amount) return -1;
    return compareKeys(left.key, right.key);
  });
  const palette = new Map();
  sorted.slice(0, TOP_CATEGORY_LIMIT).forEach((entry, index) => {
    palette.set(entry.key, CATEGORY_COLORS[index]);
  });
  return palette;
}

export function buildCategoryParents(categories = []) {
  const parents = new Map();
  for (const item of Array.isArray(categories) ? categories : []) {
    if (!item?.id || !item?.parentId) continue;
    const id = String(item.id);
    const parentId = String(item.parentId);
    if (id && parentId && id !== parentId) parents.set(id, parentId);
  }
  return parents;
}

export function categoryColorFor(item, palette = null) {
  if (!item || item.other || item.remainder) return OTHER_CATEGORY_COLOR;
  const identity = categoryColorIdentity(item);
  if (identity === "uncategorized" || identity === "other") return OTHER_CATEGORY_COLOR;
  if (palette?.has(identity)) return palette.get(identity);
  return OTHER_CATEGORY_COLOR;
}

function categoryRootId(id, parents, directParentId = "") {
  let current = id;
  const seen = new Set([current]);
  let next = directParentId || parents?.get(current) || "";
  while (next && !seen.has(next)) {
    seen.add(next);
    current = next;
    next = parents?.get(current) || "";
  }
  return current;
}

export function categoryIconColorFor(item, palette = null, parents = null) {
  if (!item || item.other || item.remainder) return OTHER_CATEGORY_COLOR;
  const identity = categoryColorIdentity(item);
  if (identity === "uncategorized" || identity === "other") return OTHER_CATEGORY_COLOR;
  if (!palette || palette.size === 0) return OTHER_CATEGORY_COLOR;
  if (palette.has(identity)) {
    const own = palette.get(identity);
    if (own && own !== OTHER_CATEGORY_COLOR) return own;
  }
  const directParentId = typeof item === "object" && item.parentId ? String(item.parentId) : "";
  const rootId = categoryRootId(identity, parents, directParentId);
  let bestRank = CATEGORY_COLORS.length;
  let bestColor = "";
  for (const [rankedId, color] of palette.entries()) {
    const rank = CATEGORY_COLORS.indexOf(color);
    if (rank < 0 || rank >= bestRank) continue;
    if (categoryRootId(String(rankedId), parents) === rootId) {
      bestRank = rank;
      bestColor = color;
    }
  }
  return bestColor || OTHER_CATEGORY_COLOR;
}

function extractLists(source) {
  if (Array.isArray(source)) {
    const positive = source.filter(item => item && !item.other && !item.remainder && (item.amount == null || parseExactAmount(item.amount) > 0n));
    return {
      primary: positive,
      secondary: [],
      history: [],
    };
  }
  return {
    primary: source?.categoryRanking?.rows || [],
    secondary: [...(source?.categoryHistory?.rows || []), ...(source?.categoryChanges || [])],
    history: source?.categoryHistory?.rows || [],
  };
}

function uniqueKeys(rows, exclude = null) {
  const seen = new Set();
  const keys = [];
  for (const row of rows) {
    const key = categoryKey(row);
    if (!key || seen.has(key) || exclude?.has(key)) continue;
    seen.add(key);
    keys.push(key);
  }
  return keys;
}

export function categoryColorMap(source, palette = null) {
  const { primary, secondary, history } = extractLists(source);
  const effectivePalette = palette && palette.size > 0 ? palette : buildCategoryPalette(primary);
  const primaryKeys = uniqueKeys(primary);
  const primarySet = new Set(primaryKeys);
  const secondaryKeys = uniqueKeys(secondary, primarySet);
  const colors = new Map();
  for (const [key, color] of effectivePalette.entries()) {
    colors.set(key, color);
  }
  for (const key of [...primaryKeys, ...secondaryKeys]) {
    if (!colors.has(key)) colors.set(key, OTHER_CATEGORY_COLOR);
  }
  const rankedKeys = [...effectivePalette.keys()];
  const rankedSet = new Set(rankedKeys);
  const remainingPrimary = primaryKeys.filter(key => !rankedSet.has(key));
  const seenOrder = new Set([...rankedKeys, ...remainingPrimary]);
  const order = [...rankedKeys, ...remainingPrimary, ...uniqueKeys(history, seenOrder)];
  return { colors, order };
}
