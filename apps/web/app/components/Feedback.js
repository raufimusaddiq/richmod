"use client";

import { useEffect, useState } from "react";

export function ErrorNotice({ message, retry }) {
  if (!message) return null;
  return <div className="notice error feedback" role="alert"><span>{message}</span>{retry && <button className="secondary" onClick={retry}>Coba lagi</button>}</div>;
}

export function Toast({ message, onClose }) {
  // `shown` outlives `message` by one animation so the exit can play; the
  // unmount is still owned by the caller through onClose.
  const [shown, setShown] = useState(message);
  const [leaving, setLeaving] = useState(false);
  // ponytail: the timer lives on the call site because a parent re-creating the
  // onClose prop restarted this effect; move it back here if onClose is stabilised.
  useEffect(() => {
    if (!message) return undefined;
    setShown(message); setLeaving(false);
    const timer = window.setTimeout(() => setLeaving(true), 3500);
    return () => window.clearTimeout(timer);
  }, [message]);
  if (!shown) return null;
  const dismiss = () => setLeaving(true);
  function settle(event) {
    if (!leaving || event.target !== event.currentTarget || event.animationName !== "toast-out") return;
    setShown(""); setLeaving(false); onClose();
  }
  return <div className={`toast${leaving ? " leaving" : ""}`} role="status" aria-live="polite" onAnimationEnd={settle}><span aria-hidden="true">✓</span>{shown}<button aria-label="Tutup notifikasi" onClick={dismiss}><span aria-hidden="true">×</span></button></div>;
}

export function Skeleton({ cards = 4, rows = 3, label = "Memuat data" }) {
  return <div className="skeleton-page" role="status" aria-live="polite" aria-label={label} aria-busy="true"><p className="skeleton-note" aria-hidden="true">{label}…</p><div className="skeleton-cards">{Array.from({ length: cards }, (_, index) => <i key={index}/>)}</div><div className="skeleton-panel">{Array.from({ length: rows }, (_, index) => <i key={index}/>)}</div></div>;
}
