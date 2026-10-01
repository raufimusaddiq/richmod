"use client";

import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import { useAuthContext } from "./AuthProvider";

const InboxCountContext = createContext(0);
export const INBOX_COUNT_EVENT = "richmod:inbox-count";

// Publish the exact number of open items from a page that already holds both
// lists, so the badge matches the inbox without another request.
export function publishInboxCount(count) {
  if (typeof window !== "undefined") window.dispatchEvent(new CustomEvent(INBOX_COUNT_EVENT, { detail: count }));
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
      setCount(items.reduce((total, value) => total + (Array.isArray(value) ? value.length : 0), 0));
      lastLoad.current = Date.now();
    } catch {
      // The badge is a convenience; the inbox page reports its own errors.
    } finally {
      loading.current = false;
    }
  }, []);

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
