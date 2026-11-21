export function mergeFreshDocuments(fresh, current) {
  const ids = new Set(fresh.map(item => item.id));
  return [...fresh, ...current.filter(item => !ids.has(item.id))];
}
