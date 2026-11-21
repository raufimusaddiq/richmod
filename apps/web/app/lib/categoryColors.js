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

export function categoryColorFor(item) {
  if (item?.other || item?.remainder) return OTHER_CATEGORY_COLOR;
  return CATEGORY_COLORS[categoryToneIndex(categoryColorIdentity(item), CATEGORY_COLORS.length)];
}

function compareKeys(left, right) {
  return left < right ? -1 : left > right ? 1 : 0;
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

export function categoryColorMap(source) {
  const { primary, secondary, history } = extractLists(source);
  const colors = new Map();
  const used = new Set();
  const assign = keys => {
    for (const key of [...keys].sort(compareKeys)) {
      let slot = categoryToneIndex(key, CATEGORY_COLORS.length);
      if (used.size < CATEGORY_COLORS.length) {
        while (used.has(slot)) slot = (slot + 1) % CATEGORY_COLORS.length;
        used.add(slot);
      }
      colors.set(key, CATEGORY_COLORS[slot]);
    }
  };
  const primaryKeys = uniqueKeys(primary);
  const primarySet = new Set(primaryKeys);
  const secondaryKeys = uniqueKeys(secondary, primarySet);
  assign(primaryKeys);
  assign(secondaryKeys);
  const order = [...primaryKeys, ...uniqueKeys(history, primarySet)];
  return { colors, order };
}
