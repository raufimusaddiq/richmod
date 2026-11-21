export const CATEGORY_COLORS = [
  "var(--chart-category-1)",
  "var(--chart-category-2)",
  "var(--chart-category-3)",
  "var(--chart-category-4)",
  "var(--chart-category-5)",
  "var(--chart-category-6)",
  "var(--chart-category-7)",
  "var(--chart-category-8)",
];

export const OTHER_CATEGORY_COLOR = "var(--chart-category-other)";

const PRIMARY_LIMIT = 7;

export function normalizeCategoryKey(value) {
  return String(value ?? "").normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLowerCase().replace(/[^a-z0-9]+/g, " ").trim();
}

export const categoryKey = item => item?.id || "uncategorized";

export function categoryColorIdentity(item) {
  if (!item) return "uncategorized";
  if (typeof item === "string") return normalizeCategoryKey(item) || "uncategorized";
  if (item.id) return String(item.id);
  return normalizeCategoryKey(item.slug || item.name) || "uncategorized";
}

export function categoryToneIndex(identity, count = CATEGORY_COLORS.length) {
  let hash = 2166136261;
  const text = String(identity || "uncategorized");
  for (let index = 0; index < text.length; index += 1) {
    hash ^= text.charCodeAt(index);
    hash = Math.imul(hash, 16777619) >>> 0;
  }
  return hash % count;
}

function compareKeys(left, right) {
  const a = String(left);
  const b = String(right);
  const ma = /^(.+?)(\d+)$/.exec(a);
  const mb = /^(.+?)(\d+)$/.exec(b);
  if (ma && mb && ma[1] === mb[1]) {
    const diff = Number(ma[2]) - Number(mb[2]);
    if (diff !== 0) return diff;
  }
  return a < b ? -1 : a > b ? 1 : 0;
}

export function buildCategoryPalette(categories = []) {
  const seen = new Set();
  const keys = [];
  for (const item of Array.isArray(categories) ? categories : []) {
    if (!item || item.other || item.remainder) continue;
    const key = categoryColorIdentity(item);
    if (!key || seen.has(key)) continue;
    seen.add(key);
    keys.push(key);
  }
  keys.sort(compareKeys);
  const palette = new Map();
  keys.forEach((key, index) => {
    palette.set(key, CATEGORY_COLORS[index % CATEGORY_COLORS.length]);
  });
  return palette;
}

export function categoryColorFor(item, palette = null) {
  if (item?.other || item?.remainder) return OTHER_CATEGORY_COLOR;
  const identity = categoryColorIdentity(item);
  if (palette?.has(identity)) return palette.get(identity);
  return CATEGORY_COLORS[categoryToneIndex(identity, CATEGORY_COLORS.length)];
}

function extractLists(source) {
  if (Array.isArray(source)) {
    const positive = source.filter(item => item && !item.other && !item.remainder && (item.amount == null || Number(item.amount) > 0));
    return {
      primary: positive.slice(0, PRIMARY_LIMIT),
      secondary: positive.slice(PRIMARY_LIMIT),
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
  const primaryKeys = uniqueKeys(primary);
  const primarySet = new Set(primaryKeys);
  const secondaryKeys = uniqueKeys(secondary, primarySet);
  const colors = new Map();
  if (palette && palette.size > 0) {
    for (const [key, color] of palette.entries()) colors.set(key, color);
  }
  for (const key of [...primaryKeys, ...secondaryKeys]) {
    if (!colors.has(key)) colors.set(key, categoryColorFor({ id: key }, palette));
  }
  const order = [...primaryKeys, ...uniqueKeys(history, primarySet)];
  return { colors, order };
}
