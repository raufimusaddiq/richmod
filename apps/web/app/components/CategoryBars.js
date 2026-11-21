import Link from "next/link";
import { CaretRight } from "@phosphor-icons/react";
import { money } from "../lib/format";
import { OTHER_CATEGORY_COLOR, categoryColorFor, categoryColorMap, categoryKey } from "../lib/categoryColors";
import { categoryLedgerHref, rankedCategories } from "../lib/home";

export default function CategoryBars({ items, window, palette = null }) {
  const rows = rankedCategories(items, 6);
  if (!rows.length) return <p className="empty compact">Belum ada pengeluaran terkonfirmasi pada periode ini.</p>;
  const { colors } = categoryColorMap(items, palette);
  const widest = rows.reduce((top, row) => Math.max(top, row.share), 0) || 1;
  return <ul className="category-bars">{rows.map(row => {
    const color = row.other ? OTHER_CATEGORY_COLOR : colors.get(categoryKey(row)) || categoryColorFor(row, palette);
    const body = <>
      <span className="category-bars-name"><i style={{ background: color }} aria-hidden="true"/>{row.name}</span>
      <span className="category-bars-value"><b>{money(row.amount)}</b><small>{Math.round(row.share * 100)}%</small></span>
      <span className="category-bars-track" aria-hidden="true"><span style={{ width: `${Math.max((row.share / widest) * 100, 3)}%`, background: color }}/></span>
      {!row.other && <CaretRight className="category-bars-go" aria-hidden="true"/>}
    </>;
    return <li key={row.id || row.name} data-other={row.other ? "true" : undefined}>{row.other ? <div className="category-bars-row">{body}</div> : <Link className="category-bars-row" href={categoryLedgerHref(row.id, window)} aria-label={`${row.name}, ${money(row.amount)}. Lihat transaksinya.`}>{body}</Link>}</li>;
  })}</ul>;
}
