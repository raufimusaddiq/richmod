"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef, useState } from "react";

const nav = [
  ["/", "Ringkasan", "⌂"],
  ["/transactions", "Transaksi", "ledger"],
  ["/analytics", "Analisis", "⌁"],
  ["/reviews", "Perlu Ditinjau", "✓"],
  ["/documents", "Dokumen", "▤"],
  ["/household", "Keluarga", "⌾"],
  ["/settings", "Pengaturan", "settings"],
];

export default function AppShell({ user, title, eyebrow, actions, children }) {
  const pathname = usePathname();
  const links = user?.isSuperAdmin ? [...nav, ["/admin", "Admin", "⚑"]] : nav;
  const [moreOpen, setMoreOpen] = useState(false);
  const moreButton = useRef(null);
  const closeMoreButton = useRef(null);
  const primaryLinks = links.slice(0, 4);
  const secondaryLinks = links.slice(4);
  useEffect(() => {
    if (!moreOpen) return;
    const closeOnEscape = event => { if (event.key === "Escape") setMoreOpen(false); };
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    document.addEventListener("keydown", closeOnEscape);
    closeMoreButton.current?.focus();
    return () => {
      document.body.style.overflow = previousOverflow;
      document.removeEventListener("keydown", closeOnEscape);
      moreButton.current?.focus();
    };
  }, [moreOpen]);
  async function logout() {
    await fetch("/api/v1/auth/logout", { method: "POST" });
    window.location.href = "/";
  }
  const navIcon = icon => icon === "ledger" ? <LedgerIcon/> : icon === "settings" ? <SettingsIcon/> : icon;
  return <div className="app-frame">
    <aside className="sidebar"><Link className="brand" href="/"><span>R</span><div>Richmod<small>Family Finance</small></div></Link><nav>{links.map(([href, label, icon]) => <Link key={href} href={href} className={pathname === href ? "active" : ""}><i>{navIcon(icon)}</i>{label}</Link>)}</nav><div className="sidebar-user"><span>{user?.displayName?.slice(0, 1) || "U"}</span><div><b>{user?.displayName}</b><small>GMT+7 · IDR</small></div><button aria-label="Keluar" onClick={logout}>↪</button></div></aside>
    <main className="app-main"><header className="page-header"><div><span className="eyebrow">{eyebrow}</span><h1>{title}</h1></div>{actions && <div className="page-actions">{actions}</div>}</header>{children}</main>
    <nav className="mobile-nav">{primaryLinks.map(([href, label, icon]) => <Link key={href} href={href} className={pathname === href ? "active" : ""}><i>{navIcon(icon)}</i><span>{label}</span></Link>)}<button ref={moreButton} type="button" className={secondaryLinks.some(([href]) => pathname === href) ? "active" : ""} aria-expanded={moreOpen} aria-controls="mobile-more-panel" onClick={() => setMoreOpen(value => !value)}><i>•••</i><span>Lainnya</span></button></nav>
    {moreOpen && <div className="mobile-more" role="dialog" aria-modal="true" aria-label="Menu lainnya" onClick={() => setMoreOpen(false)}><div id="mobile-more-panel" className="mobile-more-panel" onClick={event => event.stopPropagation()}><div className="mobile-more-header"><b>Menu lainnya</b><button ref={closeMoreButton} type="button" aria-label="Tutup menu" onClick={() => setMoreOpen(false)}>×</button></div>{secondaryLinks.map(([href, label, icon]) => <Link key={href} href={href} className={pathname === href ? "active" : ""} onClick={() => setMoreOpen(false)}><i>{navIcon(icon)}</i>{label}</Link>)}</div></div>}
  </div>;
}

function LedgerIcon() {
  return <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6.5 4.5h11a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2h-11a2 2 0 0 1-2-2v-11a2 2 0 0 1 2-2Z"/><path d="M8.5 9h7M8.5 12h7M8.5 15h4"/><path d="M4.5 8h2M4.5 12h2M4.5 16h2"/></svg>;
}

function SettingsIcon() {
  return <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 8.5a3.5 3.5 0 1 0 0 7 3.5 3.5 0 0 0 0-7Z"/><path d="m19.2 13.4 1.1.8-2 3.4-1.3-.5a7.8 7.8 0 0 1-1.8 1l-.2 1.4h-4l-.2-1.4a7.8 7.8 0 0 1-1.8-1l-1.3.5-2-3.4 1.1-.8a7.2 7.2 0 0 1 0-2.8l-1.1-.8 2-3.4 1.3.5a7.8 7.8 0 0 1 1.8-1l.2-1.4h4l.2 1.4a7.8 7.8 0 0 1 1.8 1l1.3-.5 2 3.4-1.1.8a7.2 7.2 0 0 1 0 2.8Z"/></svg>;
}
