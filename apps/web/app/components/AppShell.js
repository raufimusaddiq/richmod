"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { useInboxCount } from "./InboxCountProvider";
import { ChartDonut, DotsThree, FileText, GearSix, HouseLine, Receipt, ShieldChevron, SignOut, Tray, UsersThree, Vault, X } from "@phosphor-icons/react";

const nav = [
  ["/", "Ringkasan", "home"],
  ["/transactions", "Transaksi", "ledger"],
  ["/analytics", "Analisis", "analytics"],
  ["/wealth", "Kekayaan", "wealth"],
  ["/inbox", "Tinjauan", "inbox"],
  ["/documents", "Dokumen", "documents"],
  ["/household", "Keluarga", "household"],
  ["/settings", "Pengaturan", "settings"],
];
const icons = { home: HouseLine, ledger: Receipt, analytics: ChartDonut, wealth: Vault, inbox: Tray, documents: FileText, household: UsersThree, settings: GearSix, admin: ShieldChevron };

export default function AppShell({ user, title, eyebrow, actions, children }) {
  const pathname = usePathname();
  const links = user?.isSuperAdmin ? [...nav, ["/admin", "Admin", "admin"]] : nav;
  const [moreOpen, setMoreOpen] = useState(false);
  const inboxCount = useInboxCount();
  const moreButton = useRef(null);
  const closeMoreButton = useRef(null);
  const primaryLinks = links.slice(0, 4);
  const secondaryLinks = links.slice(4);
  const hiddenInboxCount = secondaryLinks.some(([href]) => href === "/inbox") ? inboxCount : 0;
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

  async function logout() {
    await fetch("/api/v1/auth/logout", { method: "POST" });
    window.location.href = "/";
  }

  return <div className="app-frame">
    <a className="skip-link" href="#main-content">Lewati ke konten</a>
    <aside className="sidebar">
      <Link className="brand" href="/" aria-label="Richmod Ringkasan"><span>R</span><div>Richmod<small>Keuangan keluarga</small></div></Link>
      <nav aria-label="Navigasi utama">{links.map(item => <NavLink key={item[0]} item={item} active={isActive(item[0])} inboxCount={inboxCount}/>)}</nav>
      <div className="sidebar-context"><span>Ruang kerja</span><b>{user?.householdName || "Keuangan keluarga"}</b><small>IDR · Asia/Jakarta</small></div>
      <div className="sidebar-user"><span>{user?.displayName?.slice(0, 1) || "U"}</span><div><b>{user?.displayName}</b><small>{user?.isSuperAdmin ? "Super admin" : "Anggota keluarga"}</small></div><button type="button" aria-label="Keluar" title="Keluar" onClick={logout}><SignOut aria-hidden="true"/></button></div>
    </aside>
    <main id="main-content" className="app-main"><header className="page-header"><div><span className="eyebrow">{eyebrow}</span><h1>{title}</h1></div>{actions && <div className="page-actions">{actions}</div>}</header>{children}</main>
    <nav className="mobile-nav" aria-label="Navigasi utama seluler">{primaryLinks.map(item => <NavLink key={item[0]} item={item} active={isActive(item[0])} inboxCount={inboxCount}/>)}<button ref={moreButton} type="button" aria-label={hiddenInboxCount > 0 ? `Lainnya, ${hiddenInboxCount} item menunggu tinjauan` : undefined} className={secondaryLinks.some(([href]) => isActive(href)) ? "active" : ""} aria-expanded={moreOpen} aria-controls="mobile-more-panel" onClick={() => setMoreOpen(value => !value)}><DotsThree aria-hidden="true" weight="bold"/><span>Lainnya</span>{hiddenInboxCount > 0 && !moreOpen && <b className="nav-badge" aria-hidden="true">{hiddenInboxCount}</b>}</button></nav>
    {moreOpen && <div className="mobile-more" role="dialog" aria-modal="true" aria-label="Menu lainnya" onClick={() => setMoreOpen(false)}><div id="mobile-more-panel" className="mobile-more-panel" onClick={event => event.stopPropagation()}><div className="mobile-more-header"><div><span>Ruang kerja</span><b>{user?.householdName || "Keuangan keluarga"}</b></div><button ref={closeMoreButton} type="button" aria-label="Tutup menu" onClick={() => setMoreOpen(false)}><X aria-hidden="true"/></button></div>{secondaryLinks.map(item => <NavLink key={item[0]} item={item} active={isActive(item[0])} inboxCount={inboxCount} onClick={() => setMoreOpen(false)}/>)}</div></div>}
  </div>;
}

function NavLink({ item, active, inboxCount, onClick }) {
  const [href, label, icon] = item;
  const Icon = icons[icon] || DotsThree; // the nav test guarantees every entry is mapped; this keeps a typo from crashing the shell
  const pending = href === "/inbox" && inboxCount > 0;
  return <Link href={href} className={active ? "active" : ""} aria-current={active ? "page" : undefined} aria-label={pending ? `${label}, ${inboxCount} item menunggu tinjauan` : undefined} onClick={onClick}>
    <Icon aria-hidden="true" weight={active ? "fill" : "regular"}/><span>{label}</span>
    {pending && <b className="nav-badge" aria-hidden="true">{inboxCount}</b>}
  </Link>;
}
