// Deterministic category look: an Indonesian category name or slug becomes a Phosphor icon name
// and one of the six chart category tones. No letter boxes, no randomness; an unknown category
// gets the Tag icon and a tone derived from a stable hash of its name.

// Order matters: the first rule whose keyword appears in the normalized name wins.
const RULES = [
  ["Coffee", ["kopi", "coffee", "cafe", "kafe", "snack", "jajan", "camilan", "minuman", "boba"]],
  ["Basket", ["belanja bulanan", "groceries", "grocery", "sembako", "supermarket", "pasar", "dapur"]],
  ["ForkKnife", ["makan", "food", "restoran", "resto", "kuliner", "dining", "warung", "lauk"]],
  ["House", ["rumah", "home", "housing", "kontrakan", "kos", "sewa", "cicilan rumah", "kpr", "furnitur", "perabot"]],
  ["Lightning", ["listrik", "pln", "utilitas", "utilities", "air", "pdam", "gas", "internet", "wifi", "pulsa", "token"]],
  ["Airplane", ["travel", "wisata", "liburan", "tiket pesawat", "pesawat", "hotel", "holiday"]],
  ["Car", ["transport", "bensin", "bbm", "ojek", "grab", "gojek", "parkir", "tol", "kendaraan", "mobil", "motor", "bus", "kereta", "commute"]],
  ["FirstAidKit", ["kesehatan", "health", "dokter", "obat", "apotek", "rumah sakit", "klinik", "bpjs", "medis"]],
  ["Repeat", ["langganan", "subscription", "subscribe", "netflix", "spotify", "streaming"]],
  ["Baby", ["anak", "bayi", "kids", "baby", "popok", "susu", "sekolah anak", "mainan"]],
  ["Users", ["keluarga", "family", "orang tua", "ortu", "parents", "kiriman", "transfer keluarga"]],
  ["Heart", ["donasi", "zakat", "sedekah", "amal", "sosial", "charity", "infaq"]],
  ["Ticket", ["hiburan", "entertainment", "bioskop", "konser", "game", "rekreasi", "nonton"]],
  ["ShoppingBag", ["belanja", "shopping", "fashion", "pakaian", "baju", "elektronik", "online shop", "marketplace"]],
  ["Receipt", ["tagihan", "bill", "bills", "pajak", "tax", "asuransi", "cicilan", "angsuran", "denda", "biaya admin", "admin"]],
  ["GraduationCap", ["pendidikan", "education", "kursus", "sekolah", "kuliah", "spp", "buku"]],
  ["PawPrint", ["hewan", "pet", "kucing", "anjing"]],
  ["Gift", ["hadiah", "gift", "kado", "kondangan"]],
];

export const FALLBACK_ICON = "Tag";

// Lowercase, accent-free, punctuation collapsed: "Makan & Minum" -> "makan minum".
export function normalizeCategoryName(value) {
  return String(value ?? "").normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase().replace(/[^a-z0-9]+/g, " ").trim();
}

function hashTone(text) {
  let hash = 0;
  for (const char of text) hash = (hash * 31 + char.charCodeAt(0)) >>> 0;
  return (hash % 6) + 1;
}

const matches = (text, keyword) => ` ${text} `.includes(` ${keyword} `) || (keyword.length >= 4 && text.includes(keyword));

// Accepts a category object ({ name, slug }) or a bare name.
export function categoryVisual(category) {
  const name = typeof category === "string" ? category : category?.name;
  const slug = typeof category === "string" ? "" : category?.slug;
  const haystack = normalizeCategoryName(`${name ?? ""} ${String(slug ?? "").replace(/-/g, " ")}`);
  const rule = RULES.find(([, keywords]) => keywords.some(keyword => matches(haystack, keyword)));
  const icon = rule ? rule[0] : FALLBACK_ICON;
  const tone = hashTone(rule ? rule[0] : haystack || "tag");
  return { icon, tone, color: `var(--chart-category-${tone})` };
}
