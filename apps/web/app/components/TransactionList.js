"use client";

import Link from "next/link";
import { ArrowDownLeft, ArrowUpRight, ArrowsLeftRight } from "@phosphor-icons/react";
import CategoryIcon from "./CategoryIcon";
import { useCategoryPalette } from "./CategoryPaletteProvider";
import { money, typeLabel } from "../lib/format";
import { groupByDay, timeOfDay } from "../lib/ledgerDays";
import { transactionDisplayName } from "../lib/transactionView";

const shortDay = new Intl.DateTimeFormat("id-ID", { timeZone: "Asia/Jakarta", day: "numeric", month: "short" });

function Row({ item, compact, onSelect, newId, categoryColors }) {
  const incoming = item.type === "INCOME" || item.type === "REFUND";
  const neutral = item.type === "TRANSFER" || item.type === "UNCLASSIFIED";
  const Glyph = incoming ? ArrowDownLeft : neutral ? ArrowsLeftRight : ArrowUpRight;
  const sign = incoming ? "+" : neutral ? "" : "−";
  const voided = item.status === "VOIDED", review = item.status === "NEEDS_REVIEW";
  const merchant = transactionDisplayName(item);
  const category = item.categoryName || (item.type === "TRANSFER" ? "Transfer" : item.type === "INCOME" ? "Pemasukan" : "Belum dikategorikan");
  const when = compact ? `${shortDay.format(new Date(item.transactionAt))}, ${timeOfDay(item.transactionAt)}` : timeOfDay(item.transactionAt);
  const meta = [category, when, item.accountName, item.memberName && `oleh ${item.memberName}`].filter(Boolean).join(" · ");
  const iconColor = item.categoryId ? categoryColors?.get(item.categoryId) || "" : "";
  const content = <>
    <div className="transaction-main">
      {item.categoryName ? <CategoryIcon category={{ id: item.categoryId, name: item.categoryName }} color={iconColor}/> : <span className={`transaction-icon ${item.type?.toLowerCase()}`}><Glyph aria-hidden="true"/></span>}
      <div><b>{merchant}{review && <span className="status status-needs_review">Perlu dicek</span>}{voided && <span className="status status-voided">Dibatalkan</span>}</b><small>{meta}</small></div>
    </div>
    <strong className={`transaction-amount ${incoming ? "positive" : neutral ? "neutral" : "negative"}`}>{sign} {money(item.amount)}</strong>
  </>;
  const label = `${merchant}, ${typeLabel[item.type] || item.type}, ${money(item.amount)}${voided ? ", dibatalkan" : review ? ", perlu dicek" : ""}`;
  const className = `transaction-row${item.id === newId ? " is-new" : ""}${voided ? " is-voided" : ""}`;
  return onSelect ? <button className={className} aria-label={label} onClick={() => onSelect(item)}>{content}</button> : <Link className={className} aria-label={label} href={`/transactions?id=${item.id}`}>{content}</Link>;
}

function dayTotal(day) {
  if (day.spend === "0" && day.reviewCount > 0) return `${money("0")} · ${day.reviewCount} perlu dicek`;
  return money(day.spend);
}

export default function TransactionList({ items, compact = false, onSelect, newId = "", hasFilters = false, onClearFilters, categoryColors = null }) {
  const palette = useCategoryPalette();
  const colors = categoryColors || palette;
  return <div className={`transaction-table ${compact ? "compact-table" : ""}`}>
    {compact
      ? items.map(item => <Row key={item.id} item={item} compact onSelect={onSelect} newId={newId} categoryColors={colors}/>)
      : groupByDay(items).map(day => <section className="ledger-day" key={day.day} aria-label={day.heading}>
        <header className="ledger-day-head"><h3>{day.heading}</h3><span>{dayTotal(day)}</span></header>
        {day.items.map(item => <Row key={item.id} item={item} onSelect={onSelect} newId={newId} categoryColors={colors}/>)}
      </section>)}
    {!items.length && (hasFilters
      ? <div className="empty"><p>Tidak ada transaksi yang cocok dengan filter.</p>{onClearFilters && <button type="button" className="secondary" onClick={onClearFilters}>Hapus filter</button>}</div>
      : <p className="empty">Belum ada transaksi untuk ditampilkan.</p>)}
  </div>;
}
