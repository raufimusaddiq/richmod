"use client";

import Link from "next/link";
import { dateTime, money, statusLabel, typeLabel } from "../lib/format";

export default function TransactionList({ items, compact = false, onSelect }) {
  return <div className={`transaction-table ${compact ? "compact-table" : ""}`}><div className="table-head"><span>Transaksi</span><span>Kategori / sumber</span><span>Status</span><span>Jumlah</span></div>{items.map(item => {
    const neutral = item.type === "TRANSFER" || item.type === "UNCLASSIFIED";
    const sign = item.type === "INCOME" || item.type === "REFUND" ? "+" : neutral ? "↔" : "−";
    const content = <><div className="transaction-main"><span className={`transaction-icon ${item.type?.toLowerCase()}`}>{item.type === "INCOME" ? "↓" : item.type === "EXPENSE" ? "↑" : "↔"}</span><div><b>{item.merchantName || item.description || item.counterpartyName || typeLabel[item.type] || "Transaksi"}</b><small>{dateTime(item.transactionAt)}{item.accountName ? ` · ${item.accountName}` : ""}</small></div></div><div><b>{item.categoryName || "Belum dikategorikan"}</b><small>{item.memberName || item.sourceType || "Input sistem"}</small></div><div><span className={`status status-${item.status?.toLowerCase()}`}>{statusLabel[item.status] || item.status}</span><small>{typeLabel[item.type] || item.type}</small></div><strong className={item.type === "INCOME" || item.type === "REFUND" ? "positive" : neutral ? "neutral" : "negative"}>{sign} {money(item.amount)}</strong></>;
    return onSelect ? <button className="transaction-row" key={item.id} onClick={() => onSelect(item)}>{content}</button> : <Link className="transaction-row" key={item.id} href={`/transactions?id=${item.id}`}>{content}</Link>;
  })}{!items.length && <p className="empty">Belum ada transaksi untuk ditampilkan.</p>}</div>;
}
