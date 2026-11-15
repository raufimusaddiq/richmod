// Refreshing the newest page must keep the older pages the reader already loaded. Documents are never
// deleted, so a loaded row missing from the fresh page was only pushed past it by newer uploads. Merge by id
// (never by timestamp: rows can share one), fresh rows first.
export function mergeFreshDocuments(fresh, current) {
  const ids = new Set(fresh.map(item => item.id));
  return [...fresh, ...current.filter(item => !ids.has(item.id))];
}
