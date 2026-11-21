"use client";

import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import { useAuthContext } from "./AuthProvider";
import QuickRecordDialog from "./QuickRecordDialog";
import { QUICK_EVENTS } from "../lib/quickRecord";

const QuickRecordContext = createContext({ open: () => {} });

export function openQuickRecord(detail = {}) {
  if (typeof window !== "undefined") window.dispatchEvent(new CustomEvent(QUICK_EVENTS.open, { detail }));
}

export function announceTransactionCreated(detail = {}) {
  if (typeof window !== "undefined") window.dispatchEvent(new CustomEvent(QUICK_EVENTS.created, { detail }));
}

export function useQuickRecord() {
  return useContext(QuickRecordContext);
}

const TOAST_MS = 8000;

export default function QuickRecordProvider({ children }) {
  const { user } = useAuthContext();
  const [request, setRequest] = useState(null);
  const [toast, setToast] = useState(null);
  const opener = useRef(null);
  const timer = useRef(0);
  const enabled = Boolean(user?.household);

  const open = useCallback((detail = {}) => {
    opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setRequest({ missingWindow: detail.missingWindow || null, mode: detail.mode === "photo" ? "photo" : "manual", key: Date.now() });
  }, []);
  const close = useCallback(() => {
    setRequest(null);
    const target = opener.current; opener.current = null;
    if (target?.isConnected) target.focus();
  }, []);

  useEffect(() => {
    if (!enabled) return undefined;
    const onOpen = event => open(event.detail || {});
    window.addEventListener(QUICK_EVENTS.open, onOpen);
    return () => window.removeEventListener(QUICK_EVENTS.open, onOpen);
  }, [enabled, open]);

  const dismiss = useCallback(() => { window.clearTimeout(timer.current); setToast(null); }, []);
  useEffect(() => () => window.clearTimeout(timer.current), []);

  function saved(body) {
    window.clearTimeout(timer.current);
    setToast({ id: body?.id || "", text: "Tercatat", undoing: false });
    timer.current = window.setTimeout(() => setToast(null), TOAST_MS);
    announceTransactionCreated({ id: body?.id || "" });
    close();
  }

  async function undo() {
    if (!toast?.id || toast.undoing) return;
    window.clearTimeout(timer.current);
    setToast(current => current && { ...current, undoing: true });
    try {
      const response = await fetch(`/api/v1/transactions/${toast.id}/void`, { method: "POST" });
      if (!response.ok) throw new Error("void failed");
      announceTransactionCreated({ id: toast.id, voided: true });
      setToast({ id: "", text: "Dibatalkan", undoing: false });
    } catch {
      setToast({ id: toast.id, text: "Belum bisa dibatalkan. Coba lagi.", undoing: false });
    }
    timer.current = window.setTimeout(() => setToast(null), TOAST_MS);
  }

  return <QuickRecordContext.Provider value={{ open }}>
    {children}
    {enabled && request && <QuickRecordDialog key={request.key} request={request} onClose={close} onSaved={saved}/>}
    {toast && <div className="toast quick-toast" role="status" aria-live="polite"><span aria-hidden="true">✓</span><b>{toast.text}</b>{toast.id && <button type="button" className="toast-undo" disabled={toast.undoing} onClick={undo}>{toast.undoing ? "Membatalkan…" : "Urungkan"}</button>}<button type="button" aria-label="Tutup notifikasi" onClick={dismiss}><span aria-hidden="true">×</span></button></div>}
  </QuickRecordContext.Provider>;
}
