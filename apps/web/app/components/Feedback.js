"use client";

import { useEffect } from "react";

export function ErrorNotice({ message, retry }) {
  if (!message) return null;
  return <div className="notice error feedback" role="alert"><span>{message}</span>{retry && <button className="secondary" onClick={retry}>Coba lagi</button>}</div>;
}

export function Toast({ message, onClose }) {
  // ponytail: the timer lives on the call site because a parent re-creating the
  // onClose prop restarted this effect; move it back here if onClose is stabilised.
  useEffect(() => {
    if (!message) return undefined;
    const timer = window.setTimeout(() => onClose(), 3500);
    return () => window.clearTimeout(timer);
  }, [message]);
  if (!message) return null;
  return <div className="toast" role="status" aria-live="polite"><span aria-hidden="true">✓</span>{message}<button aria-label="Tutup notifikasi" onClick={onClose}><span aria-hidden="true">×</span></button></div>;
}

export function Skeleton({ cards = 4, rows = 3, label = "Memuat data" }) {
  return <div className="skeleton-page" role="status" aria-live="polite" aria-label={label} aria-busy="true"><p className="skeleton-note" aria-hidden="true">{label}…</p><div className="skeleton-cards">{Array.from({ length: cards }, (_, index) => <i key={index}/>)}</div><div className="skeleton-panel">{Array.from({ length: rows }, (_, index) => <i key={index}/>)}</div></div>;
}
