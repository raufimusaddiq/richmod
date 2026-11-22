"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { useAuthContext } from "./AuthProvider";

const InboxCountContext = createContext({ reviewCount: 0, actionCount: 0, total: 0 });
const INBOX_COUNT_EVENT = "richmod:inbox-count";

export function publishInboxCount(count) {
  if (typeof window !== "undefined") window.dispatchEvent(new CustomEvent(INBOX_COUNT_EVENT, { detail: count }));
}

export function applyBadge(target, count) {
  if (!target) return;
  const value = Number.isFinite(count) ? count : 0;
  try {
    if (value > 0) {
      if (typeof target.setAppBadge === "function") Promise.resolve(target.setAppBadge(value)).catch(() => {});
      return;
    }
    if (typeof target.clearAppBadge === "function") Promise.resolve(target.clearAppBadge()).catch(() => {});
  } catch {
  }
}

export default function InboxCountProvider({ children }) {
  const { user } = useAuthContext();
  const [counts, setCounts] = useState({ reviewCount: 0, actionCount: 0 });
  const lastLoad = useRef(0);
  const loading = useRef(false);
  const signedIn = Boolean(user?.household);

  const load = useCallback(async () => {
    if (loading.current) return;
    loading.current = true;
    try {
      const responses = await Promise.all([fetch("/api/v1/reviews"), fetch("/api/v1/integration-actions")]);
      if (!responses.every(response => response.ok)) return;
      const items = await Promise.all(responses.map(response => response.json()));
      if (!Array.isArray(items[1])) return;
      const header = Number.parseInt(responses[0].headers.get("X-Decision-Count") || "", 10);
      const reviews = Number.isFinite(header) ? header : (Array.isArray(items[0]) ? items[0].filter(item => item.presentation !== "NOTICE").length : null);
      if (reviews === null) return;
      setCounts({ reviewCount: reviews, actionCount: items[1].length });
      lastLoad.current = Date.now();
    } catch {
    } finally {
      loading.current = false;
    }
  }, []);

  const total = counts.reviewCount + counts.actionCount;
  useEffect(() => { applyBadge(typeof navigator === "undefined" ? null : navigator, signedIn ? total : 0); }, [total, signedIn]);

  useEffect(() => {
    if (!signedIn) { setCounts({ reviewCount: 0, actionCount: 0 }); return undefined; }
    load();
    const onFocus = () => { if (Date.now() - lastLoad.current > 60000) load(); };
    const onPublish = event => {
      const next = event.detail || {};
      if (!Number.isFinite(next.reviewCount) && !Number.isFinite(next.actionCount)) return;
      setCounts(previous => ({ reviewCount: Number.isFinite(next.reviewCount) ? next.reviewCount : previous.reviewCount, actionCount: Number.isFinite(next.actionCount) ? next.actionCount : previous.actionCount }));
      lastLoad.current = Date.now();
    };
    window.addEventListener("focus", onFocus);
    window.addEventListener(INBOX_COUNT_EVENT, onPublish);
    return () => { window.removeEventListener("focus", onFocus); window.removeEventListener(INBOX_COUNT_EVENT, onPublish); };
  }, [signedIn, load]);

  const value = useMemo(() => ({ ...counts, total }), [counts, total]);
  return <InboxCountContext.Provider value={value}>{children}</InboxCountContext.Provider>;
}

export function useInboxCount() {
  return useContext(InboxCountContext).reviewCount;
}

export function useInboxCounts() {
  return useContext(InboxCountContext);
}
