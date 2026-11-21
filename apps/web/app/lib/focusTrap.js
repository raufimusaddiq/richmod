export const focusableSelector = "a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex]:not([tabindex='-1'])";

export function trapTarget(items, active, shiftKey, container) {
  if (!items.length) return container || null;
  const first = items[0], last = items[items.length - 1];
  const outside = !items.includes(active);
  if (shiftKey) return outside || active === first ? last : null;
  return outside || active === last ? first : null;
}

export function trapKeyDown(event, container, close) {
  if (event.key === "Escape") { close(); return; }
  if (event.key !== "Tab" || !container) return;
  const items = [...container.querySelectorAll(focusableSelector)];
  const target = trapTarget(items, document.activeElement, event.shiftKey, container);
  if (target) { event.preventDefault(); target.focus(); }
}

export function nextTabIndex(key, index, count) {
  if (!count) return null;
  if (key === "ArrowRight") return (index + 1) % count;
  if (key === "ArrowLeft") return (index - 1 + count) % count;
  if (key === "Home") return 0;
  if (key === "End") return count - 1;
  return null;
}
