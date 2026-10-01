"use client";

import CanonicalCard from "./review/CanonicalCard";
import ReviewCard from "./review/ReviewCard";
import TransferCard from "./review/TransferCard";

export default function ReviewCards({ items, categories, accounts = [], wealthAccounts = [], working, action }) {
  return <div className="review-grid">{items.map(item => item.subjectType ? <CanonicalCard key={item.id} item={item} categories={categories} accounts={accounts} wealthAccounts={wealthAccounts} disabled={working === item.id} action={action}/> : item.type === "UNCLASSIFIED" ? <TransferCard key={item.id} item={item} categories={categories} wealthAccounts={wealthAccounts} disabled={working === item.id} action={action}/> : <ReviewCard key={item.id} item={item} categories={categories} wealthAccounts={wealthAccounts} disabled={working === item.id} action={action}/>)}{!items.length && <div className="empty-state"><span aria-hidden="true">✓</span><h2>Kotak tinjauan sudah bersih</h2><p>Tidak ada transaksi yang membutuhkan keputusan saat ini.</p></div>}</div>;
}
