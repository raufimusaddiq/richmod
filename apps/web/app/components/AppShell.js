import { Link } from "../router";
import { usePathname } from "../router";
import { useEffect, useRef, ViewTransition } from "react";
import { useInboxCount } from "./InboxCountProvider";
import { useQuickRecord } from "./QuickRecordProvider";
import BuildUpdateNotice from "./BuildUpdateNotice";
import NoHouseholdIntro from "./NoHouseholdIntro";
import useRouteReveal from "./useRouteReveal";
import { trackScreenView } from "../lib/telemetry";
import { ChartDonut, DotsThree, HouseLine, Plus, Receipt, ShieldChevron, SignOut, Tray, UsersThree } from "@phosphor-icons/react";

const nav = [
  ["/", "Beranda", "home"],
  ["/transactions", "Transaksi", "ledger"],
  ["/analytics", "Laporan", "analytics"],
  ["/household", "Keluarga", "household"],
];
const inboxItem = ["/inbox", "Perlu dicek", "inbox"];
const icons = { home: HouseLine, ledger: Receipt, analytics: ChartDonut, household: UsersThree, inbox: Tray, admin: ShieldChevron };

const ALIASES = { "/wealth": "/analytics", "/settings": "/household" };
export function activeHref(pathname) {
  const path = ALIASES[Object.keys(ALIASES).find(prefix => pathname.startsWith(prefix))] || pathname;
  return path;
}

export default function AppShell({ user, title, eyebrow, actions, children }) {
  const pathname = usePathname();
  const reviewCount = useInboxCount();
  const { open } = useQuickRecord();
  const main = useRef(null);
  useRouteReveal(main);
  useEffect(() => { trackScreenView(pathname); }, [pathname]);
  const current = activeHref(pathname);
  const isActive = href => href === "/" ? current === href : current.startsWith(href);
  const [homeLink, ledgerLink, reportLink, familyLink] = nav;

  async function logout() {
    await fetch("/api/v1/auth/logout", { method: "POST" });
    window.location.href = "/";
  }

  if (user && !user.household && !pathname.startsWith("/admin")) return <NoHouseholdIntro user={user}/>;

  return <div className="app-frame">
    <a className="skip-link" href="#main-content">Lewati ke konten</a>
    <aside className="sidebar">
      <Link className="brand" href="/" aria-label="Richmod Beranda"><span>R</span><div>Richmod<small>Keuangan keluarga</small></div></Link>
      <button type="button" className="quick-cta" onClick={() => open()}><Plus aria-hidden="true" weight="bold"/><span>Catat</span></button>
      <nav aria-label="Navigasi utama">
        {reviewCount > 0 && <NavLink item={inboxItem} active={isActive("/inbox")} inboxCount={reviewCount} className="needs-review"/>}
        {nav.map(item => <NavLink key={item[0]} item={item} active={isActive(item[0])} inboxCount={reviewCount}/>)}
      </nav>
      <div className="sidebar-context"><span>Ruang kerja</span><b>{user?.householdName || "Keuangan keluarga"}</b><small>IDR · Asia/Jakarta</small></div>
      {user?.isSuperAdmin && <Link className="admin-link" href="/admin" aria-current={isActive("/admin") ? "page" : undefined}><ShieldChevron aria-hidden="true"/><span>Admin</span></Link>}
      <div className="sidebar-user"><span>{user?.displayName?.slice(0, 1) || "U"}</span><div><b>{user?.displayName}</b><small>{user?.isSuperAdmin ? "Super admin" : "Anggota keluarga"}</small></div><button type="button" aria-label="Keluar" title="Keluar" onClick={logout}><SignOut aria-hidden="true"/></button></div>
    </aside>
    <ViewTransition name="app-main" enter="page-swap" exit="page-swap" share="page-swap" default="none"><main ref={main} id="main-content" className="app-main"><BuildUpdateNotice/><header className="page-header"><div><span className="eyebrow">{eyebrow}</span><h1>{title}</h1></div>{(actions || reviewCount > 0) && <div className="page-actions">{reviewCount > 0 && <Link className="review-pill" href="/inbox">{reviewCount} perlu dicek ›</Link>}{actions}</div>}</header>{children}</main></ViewTransition>
    <nav className="mobile-nav" aria-label="Navigasi utama seluler">
      <NavLink item={homeLink} active={isActive(homeLink[0])} inboxCount={reviewCount}/>
      <NavLink item={ledgerLink} active={isActive(ledgerLink[0])} inboxCount={reviewCount}/>
      <button type="button" className="mobile-add" aria-label="Catat transaksi" onClick={() => open()}><Plus aria-hidden="true" weight="bold"/></button>
      <NavLink item={reportLink} active={isActive(reportLink[0])} inboxCount={reviewCount}/>
      <NavLink item={familyLink} active={isActive(familyLink[0])} inboxCount={reviewCount}/>
    </nav>
  </div>;
}

function NavLink({ item, active, inboxCount, className = "", onClick }) {
  const [href, label, icon] = item;
  const Icon = icons[icon] || DotsThree;
  const pending = href === "/inbox" && inboxCount > 0;
  return <Link href={href} className={`${active ? "active" : ""} ${className}`.trim()} aria-current={active ? "page" : undefined} aria-label={pending ? `${label}, ${inboxCount} item menunggu keputusan` : undefined} onClick={onClick}>
    <Icon aria-hidden="true" weight={active ? "fill" : "regular"}/><span>{label}</span>
    {pending && <b className="nav-badge" aria-hidden="true">{inboxCount}</b>}
  </Link>;
}
