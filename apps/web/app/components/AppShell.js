"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const nav = [
  ["/", "Overview", "⌂"],
  ["/transactions", "Transactions", "ledger"],
  ["/analytics", "Analytics", "⌁"],
  ["/reviews", "Review Inbox", "✓"],
  ["/documents", "Documents", "▤"],
  ["/household", "Household", "⌾"],
  ["/settings", "Settings", "⚙"],
];

export default function AppShell({ user, title, eyebrow, actions, children }) {
  const pathname = usePathname();
  async function logout() {
    await fetch("/api/v1/auth/logout", { method: "POST" });
    window.location.href = "/";
  }
  return <div className="app-frame">
    <aside className="sidebar"><Link className="brand" href="/"><span>R</span><div>Richmod<small>Family Finance</small></div></Link><nav>{nav.map(([href, label, icon]) => <Link key={href} href={href} className={pathname === href ? "active" : ""}><i>{icon === "ledger" ? <LedgerIcon/> : icon}</i>{label}</Link>)}</nav><div className="sidebar-user"><span>{user?.displayName?.slice(0, 1) || "U"}</span><div><b>{user?.displayName}</b><small>GMT+7 · IDR</small></div><button aria-label="Keluar" onClick={logout}>↪</button></div></aside>
    <main className="app-main"><header className="page-header"><div><span className="eyebrow">{eyebrow}</span><h1>{title}</h1></div>{actions && <div className="page-actions">{actions}</div>}</header>{children}</main>
    <nav className="mobile-nav">{nav.slice(0, 5).map(([href, label, icon]) => <Link key={href} href={href} className={pathname === href ? "active" : ""}><i>{icon === "ledger" ? <LedgerIcon/> : icon}</i><span>{label === "Transactions" ? "Ledger" : label === "Review Inbox" ? "Review" : label}</span></Link>)}</nav>
  </div>;
}

function LedgerIcon() {
  return <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6.5 4.5h11a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2h-11a2 2 0 0 1-2-2v-11a2 2 0 0 1 2-2Z"/><path d="M8.5 9h7M8.5 12h7M8.5 15h4"/><path d="M4.5 8h2M4.5 12h2M4.5 16h2"/></svg>;
}
