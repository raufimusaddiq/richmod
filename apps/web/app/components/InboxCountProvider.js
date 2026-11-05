"use client";

import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import { useAuthContext } from "./AuthProvider";

const InboxCountContext = createContext(0);
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

// One shared count for the whole session. The shell used to download both full
// lists on every navigation; now it loads once, again when the tab regains
// focus (at most once a minute), and whenever the inbox publishes a new count.
export default function InboxCountProvider({ children }) {
  const { user } = useAuthContext();
  const [count, setCount] = useState(0);
  const lastLoad = useRef(0);
  const loading = useRef(false);
  const signedIn = Boolean(user);

  const load = useCallback(async () => {
    if (loading.current) return;
    loading.current = true;
    try {
      const responses = await Promise.all([fetch("/api/v1/reviews"), fetch("/api/v1/integration-actions")]);
      const items = await Promise.all(responses.map(response => response.ok ? response.json() : []));
      // Reviews report their needs-your-decision count (all pages, notices
      // excluded) in X-Decision-Count; a list without it counts its decisions.
      const header = Number.parseInt(responses[0].ok ? responses[0].headers.get("X-Decision-Count") || "" : "", 10);
      const reviews = Number.isFinite(header) ? header : (Array.isArray(items[0]) ? items[0].filter(item => item.presentation !== "NOTICE").length : 0);
      setCount(reviews + (Array.isArray(items[1]) ? items[1].length : 0));
      lastLoad.current = Date.now();
    } catch {
      // The badge is a convenience; the inbox page reports its own errors.
    } finally {
      loading.current = false;
    }
  }, []);

  useEffect(() => { applyBadge(typeof navigator === "undefined" ? null : navigator, signedIn ? count : 0); }, [count, signedIn]);

  useEffect(() => {
    if (!signedIn) { setCount(0); return undefined; }
    load();
    const onFocus = () => { if (Date.now() - lastLoad.current > 60000) load(); };
    const onPublish = event => { if (Number.isFinite(event.detail)) { setCount(event.detail); lastLoad.current = Date.now(); } };
    window.addEventListener("focus", onFocus);
    window.addEventListener(INBOX_COUNT_EVENT, onPublish);
    return () => { window.removeEventListener("focus", onFocus); window.removeEventListener(INBOX_COUNT_EVENT, onPublish); };
  }, [signedIn, load]);

  return <InboxCountContext.Provider value={count}>{children}</InboxCountContext.Provider>;
}

export function useInboxCount() {
  return useContext(InboxCountContext);
}
