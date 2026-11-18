// Category slug from a display name. The API still requires a slug, so the form
// derives it instead of asking the family to type one: lowercase ASCII, words
// joined by single hyphens. Accents are folded, symbols dropped.
export function categorySlug(name) {
  return String(name || "")
    .normalize("NFD")
    .replace(/[\u0300-\u036f]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "");
}

// A name that is already taken under the same parent gets -2, -3, ... so the
// server's uniqueness index is not the first thing the user hears about.
export function uniqueCategorySlug(name, taken = []) {
  const base = categorySlug(name) || "kategori";
  const used = new Set(taken);
  if (!used.has(base)) return base;
  let suffix = 2;
  while (used.has(`${base}-${suffix}`)) suffix += 1;
  return `${base}-${suffix}`;
}
