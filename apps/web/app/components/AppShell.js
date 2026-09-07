"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { ChartDonut, DotsThree, FileText, GearSix, HouseLine, Receipt, ShieldChevron, SignOut, Tray, UsersThree, X } from "@phosphor-icons/react";

const nav = [
  ["/", "Ringkasan", "⌂"],
  ["/transactions", "Transaksi", "ledger"],
  ["/analytics", "Analisis", "⌁"],
  ["/inbox", "Inbox", "✓"],
  ["/documents", "Dokumen", "▤"],
  ["/household", "Keluarga", "⌾"],
  ["/settings", "Pengaturan", "settings"],
];
const icons = { "⌂": HouseLine, ledger: Receipt, "⌁": ChartDonut, "✓": Tray, "▤": FileText, "⌾": UsersThree, settings: GearSix, admin: ShieldChevron };

export default function AppShell({ user, title, eyebrow, actions, children }) {
  const pathname = usePathname();
  const links = user?.isSuperAdmin ? [...nav, ["/admin", "Admin", "admin"]] : nav;
  const [moreOpen, setMoreOpen] = useState(false);
  const [inboxCount, setInboxCount] = useState(0);
  const moreButton = useRef(null);
  const closeMoreButton = useRef(null);
  const primaryLinks = links.slice(0, 4);
  const secondaryLinks = links.slice(4);
  const isActive = href => href === "/" ? pathname === href : pathname.startsWith(href);

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

  useEffect(() => {
    Promise.all([fetch("/api/v1/reviews"), fetch("/api/v1/integration-actions")])
      .then(async responses => {
        const items = await Promise.all(responses.map(response => response.ok ? response.json() : []));
        setInboxCount(items.reduce((total, value) => total + (Array.isArray(value) ? value.length : 0), 0));
      })
      .catch(() => {});
  }, [pathname]);

  async function logout() {
    await fetch("/api/v1/auth/logout", { method: "POST" });
    window.location.href = "/";
  }

  function NavLink({ item, onClick }) {
    const [href, label, icon] = item;
    const Icon = icons[icon];
    const active = isActive(href);
    return <Link href={href} className={active ? "active" : ""} aria-current={active ? "page" : undefined} onClick={onClick}>
      <Icon aria-hidden="true" weight={active ? "fill" : "regular"}/><span>{label}</span>
      {href === "/inbox" && inboxCount > 0 && <b className="nav-badge" aria-label={`${inboxCount} item inbox`}>{inboxCount}</b>}
    </Link>;
  }

  return <div className="app-frame">
    <a className="skip-link" href="#main-content">Lewati ke konten</a>
    <aside className="sidebar">
      <Link className="brand" href="/" aria-label="Richmod Ringkasan"><span>R</span><div>Richmod<small>Household finance</small></div></Link>
      <nav aria-label="Navigasi utama">{links.map(item => <NavLink key={item[0]} item={item}/>)}</nav>
      <div className="sidebar-context"><span>Ruang kerja</span><b>{user?.householdName || "Keuangan keluarga"}</b><small>IDR · Asia/Jakarta</small></div>
      <div className="sidebar-user"><span>{user?.displayName?.slice(0, 1) || "U"}</span><div><b>{user?.displayName}</b><small>{user?.isSuperAdmin ? "Super admin" : "Anggota keluarga"}</small></div><button type="button" aria-label="Keluar" title="Keluar" onClick={logout}><SignOut aria-hidden="true"/></button></div>
    </aside>
    <main id="main-content" className="app-main"><header className="page-header"><div><span className="eyebrow">{eyebrow}</span><h1>{title}</h1></div>{actions && <div className="page-actions">{actions}</div>}</header>{children}</main>
    <nav className="mobile-nav" aria-label="Navigasi utama seluler">{primaryLinks.map(item => <NavLink key={item[0]} item={item}/>)}<button ref={moreButton} type="button" className={secondaryLinks.some(([href]) => isActive(href)) ? "active" : ""} aria-expanded={moreOpen} aria-controls="mobile-more-panel" onClick={() => setMoreOpen(value => !value)}><DotsThree aria-hidden="true" weight="bold"/><span>Lainnya</span></button></nav>
    {moreOpen && <div className="mobile-more" role="dialog" aria-modal="true" aria-label="Menu lainnya" onClick={() => setMoreOpen(false)}><div id="mobile-more-panel" className="mobile-more-panel" onClick={event => event.stopPropagation()}><div className="mobile-more-header"><div><span>Ruang kerja</span><b>{user?.householdName || "Keuangan keluarga"}</b></div><button ref={closeMoreButton} type="button" aria-label="Tutup menu" onClick={() => setMoreOpen(false)}><X aria-hidden="true"/></button></div>{secondaryLinks.map(item => <NavLink key={item[0]} item={item} onClick={() => setMoreOpen(false)}/>)}</div></div>}
  </div>;
}
