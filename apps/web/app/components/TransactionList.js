"use client";

import Link from "next/link";
import { ArrowDownLeft, ArrowUpRight, ArrowsLeftRight } from "@phosphor-icons/react";
import { dateTime, money, statusLabel, typeLabel } from "../lib/format";

export default function TransactionList({ items, compact = false, onSelect }) {
  return <div className={`transaction-table ${compact ? "compact-table" : ""}`}>
    <div className="table-head"><span>Transaksi</span><span>Kategori / sumber</span><span>Status</span><span>Jumlah</span></div>
    {items.map(item => {
      const incoming = item.type === "INCOME" || item.type === "REFUND";
      const neutral = item.type === "TRANSFER" || item.type === "UNCLASSIFIED";
      const Icon = incoming ? ArrowDownLeft : neutral ? ArrowsLeftRight : ArrowUpRight;
      const sign = incoming ? "+" : neutral ? "" : "−";
      const merchant = item.merchantName || item.description || item.counterpartyName || typeLabel[item.type] || "Transaksi";
      const content = <>
        <div className="transaction-main"><span className={`transaction-icon ${item.type?.toLowerCase()}`}><Icon aria-hidden="true"/></span><div><b>{merchant}</b><small>{dateTime(item.transactionAt)}{item.accountName ? ` · ${item.accountName}` : ""}</small></div></div>
        <div className="transaction-meta"><b>{item.categoryName || "Belum dikategorikan"}</b><small>{item.memberName || item.sourceType || "Input sistem"}</small></div>
        <div className="transaction-state"><span className={`status status-${item.status?.toLowerCase()}`}>{statusLabel[item.status] || item.status}</span><small>{typeLabel[item.type] || item.type}</small></div>
        <strong className={`transaction-amount ${incoming ? "positive" : neutral ? "neutral" : "negative"}`}>{sign} {money(item.amount)}</strong>
      </>;
      const label = `${merchant}, ${typeLabel[item.type] || item.type}, ${money(item.amount)}`;
      return onSelect ? <button className="transaction-row" key={item.id} aria-label={label} onClick={() => onSelect(item)}>{content}</button> : <Link className="transaction-row" key={item.id} aria-label={label} href={`/transactions?id=${item.id}`}>{content}</Link>;
    })}
    {!items.length && <p className="empty">Belum ada transaksi untuk ditampilkan.</p>}
  </div>;
}
