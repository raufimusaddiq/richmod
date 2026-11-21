"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { useAuthContext } from "./AuthProvider";

// reviewCount: money decisions ("Perlu dicek"). actionCount: owner integration setup
// actions (e.g. email forwarding verification), shown under Keluarga > Sumber otomatis.
const InboxCountContext = createContext({ reviewCount: 0, actionCount: 0, total: 0 });
const INBOX_COUNT_EVENT = "richmod:inbox-count";

// Publish the exact number of open items from a page that already holds both
// lists, so the badge matches the inbox without another request.
export function publishInboxCount(count) {
  if (typeof window !== "undefined") window.dispatchEvent(new CustomEvent(INBOX_COUNT_EVENT, { detail: count }));
}

// Mirror the one shared count onto the installed icon where the Badging API
// exists. It is a no-op elsewhere; nothing else may keep a second count.
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
    // Badging is decorative; a refusal must not break the app.
  }
}

// One shared count for the whole session: loaded once, again when the tab regains focus
// (at most once a minute), and whenever the inbox publishes a new count.
export default function InboxCountProvider({ children }) {
  const { user } = useAuthContext();
  const [counts, setCounts] = useState({ reviewCount: 0, actionCount: 0 });
  const lastLoad = useRef(0);
  const loading = useRef(false);
  const signedIn = Boolean(user);

  const load = useCallback(async () => {
    if (loading.current) return;
    loading.current = true;
    try {
      const responses = await Promise.all([fetch("/api/v1/reviews"), fetch("/api/v1/integration-actions")]);
      // A 5xx is "unknown", not "zero": keep the last known count.
      if (!responses.every(response => response.ok)) return;
      const items = await Promise.all(responses.map(response => response.json()));
      if (!Array.isArray(items[1])) return;
      // Reviews report their needs-your-decision count (all pages, notices
      // excluded) in X-Decision-Count; a list without it counts its decisions.
      const header = Number.parseInt(responses[0].headers.get("X-Decision-Count") || "", 10);
      const reviews = Number.isFinite(header) ? header : (Array.isArray(items[0]) ? items[0].filter(item => item.presentation !== "NOTICE").length : null);
      if (reviews === null) return;
      setCounts({ reviewCount: reviews, actionCount: items[1].length });
      lastLoad.current = Date.now();
    } catch {
      // The badge is a convenience; the inbox page reports its own errors.
    } finally {
      loading.current = false;
    }
  }, []);

  const total = counts.reviewCount + counts.actionCount;
  // The installed-app icon badge covers everything waiting on a person.
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

// "Perlu dicek": money decisions only.
export function useInboxCount() {
  return useContext(InboxCountContext).reviewCount;
}

// reviewCount and actionCount apart; total is what the installed-app badge shows.
export function useInboxCounts() {
  return useContext(InboxCountContext);
}
