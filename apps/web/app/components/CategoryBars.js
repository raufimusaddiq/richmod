import Link from "next/link";
import { CaretRight } from "@phosphor-icons/react";
import { money } from "../lib/format";
import { categoryVisual } from "../lib/categoryIcons";
import { categoryLedgerHref, rankedCategories } from "../lib/home";

// "Ke mana uangnya": ranked horizontal bars over the same window as the hero. Each named category
// opens the ledger filtered to that category and window; the "Kategori lain" bucket is a plain row.
export default function CategoryBars({ items, window }) {
  const rows = rankedCategories(items, 6);
  if (!rows.length) return <p className="empty compact">Belum ada pengeluaran terkonfirmasi pada periode ini.</p>;
  const widest = rows.reduce((top, row) => Math.max(top, row.share), 0) || 1;
  return <ul className="category-bars">{rows.map(row => {
    const color = row.other ? "var(--line-strong)" : categoryVisual(row.name).color;
    const body = <>
      <span className="category-bars-name"><i style={{ background: color }} aria-hidden="true"/>{row.name}</span>
      <span className="category-bars-value"><b>{money(row.amount)}</b><small>{Math.round(row.share * 100)}%</small></span>
      <span className="category-bars-track" aria-hidden="true"><span style={{ width: `${Math.max((row.share / widest) * 100, 3)}%`, background: color }}/></span>
      {!row.other && <CaretRight className="category-bars-go" aria-hidden="true"/>}
    </>;
    return <li key={row.id || row.name}>{row.other ? <div className="category-bars-row">{body}</div> : <Link className="category-bars-row" href={categoryLedgerHref(row.id, window)} aria-label={`${row.name}, ${money(row.amount)}. Lihat transaksinya.`}>{body}</Link>}</li>;
  })}</ul>;
}
