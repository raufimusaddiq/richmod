import Link from "next/link";
import { useState } from "react";
import { dateTime, money } from "../../lib/format";
import { reasons, MissingInputs, ProposalFacts } from "./shared";

export default function ReviewCard({ item, categories, wealthAccounts, disabled, action }) {
  const [editing, setEditing] = useState(false);
  const [categoryRequired, setCategoryRequired] = useState(false);
  async function confirm(event) {
    event.preventDefault();
    const element = event.currentTarget, form = new FormData(element);
    const result = await action(item.id, "confirm", { categoryId: form.get("categoryId") || null, merchantName: form.get("merchantName") || null, transactionAt: form.get("transactionAt") || null, note: form.get("note") || null, rememberMerchant: form.get("rememberMerchant") === "on" });
    if (result?.missingFacts?.includes("category")) {
      setCategoryRequired(true);
      element.elements.categoryId?.focus();
    }
  }
  function assetPurchase(event) { event.preventDefault(); const form = new FormData(event.currentTarget); action(item.id, "classify-transfer", { classification: "ASSET_PURCHASE", wealthAccountId: form.get("wealthAccountId") }); }
  const missing = MissingInputs(item);
  const merchantRecallAllowed = missing.merchant && !categoryRequired;
  const proposed = item.proposedFacts || {};
  const proposalLabel = proposed.categoryId ? (categories.find(category => category.id === proposed.categoryId)?.name || proposed.categoryId) : (proposed.categorySlug || null);
  // A legacy card (no decision contract) has no proposal to accept, and the
  // server rejects an EXPENSE without a category and a bank EXPENSE without a
  // merchant. Offering "Benar, simpan" there posts a request that can only 400,
  // so the primary action opens the form instead.
  const quickCategoryId = item.categoryId || proposed.categoryId || null;
  const canQuickAccept = item.type === "EXPENSE" ? Boolean(quickCategoryId) && !missing.merchant && !missing.category && !missing.transactionAt : !missing.transactionAt;
  // The proposal is the default reading of the card; editing is a deliberate
  // second step and only requests facts the decision left open.
  return <article className="review-card"><div className="review-top"><span className="review-reason">{reasons[item.reason] || item.reason}</span><strong>{money(item.amount)}</strong></div><h2>{item.merchantName || item.counterparty || item.description || "Transaksi tanpa keterangan"}</h2><p>{dateTime(item.transactionAt)} · {item.sourceType || "Input"}</p><ProposalFacts item={item} known={[["amount_idr", money(item.amount)], ["merchant", item.merchantName || item.counterparty || item.description], ["category", proposalLabel]]} missing={item.missingFacts || []}/>{item.candidates?.length > 0 && <details className="review-evidence"><summary>Bukti</summary><div className="candidates">{item.candidates.map(candidate => <button type="button" disabled={disabled} key={candidate.id} onClick={() => action(item.id, "merge", { targetTransactionId: candidate.id })}><span>{dateTime(candidate.transactionAt)} · {candidate.description || "Transaksi sebelumnya"}</span><strong>{money(candidate.amount)} · Gabungkan</strong></button>)}</div></details>}{!editing ? <div className="review-actions">{canQuickAccept ? <button disabled={disabled} onClick={() => action(item.id, "confirm", { categoryId: quickCategoryId })}>Benar, simpan</button> : <button disabled={disabled} onClick={() => setEditing(true)}>Benar, lanjut isi detail</button>}<button disabled={disabled} onClick={() => setEditing(true)}>Ubah detail</button><button className="danger" type="button" disabled={disabled} onClick={() => action(item.id, "reject")}>Abaikan</button><Link className="button secondary" href={`/transactions?id=${item.id}`}>Bukti</Link></div> : <form onSubmit={confirm}>{missing.merchant && <label>Merchant<input name="merchantName" required defaultValue={item.merchantName || ""} maxLength="160" placeholder="Contoh: Alfamart" onChange={() => setCategoryRequired(false)}/></label>}{missing.category && <label>Kategori<select name="categoryId" defaultValue={item.categoryId || ""} required={item.type === "EXPENSE" && !merchantRecallAllowed} aria-invalid={categoryRequired || undefined} aria-describedby={categoryRequired ? `category-error-${item.id}` : undefined}><option value="" disabled={!merchantRecallAllowed}>{merchantRecallAllowed ? "Gunakan kategori merchant tersimpan" : "Pilih kategori"}</option>{categories.filter(category => category.active).map(category => <option key={category.id} value={category.id}>{category.name}</option>)}</select>{categoryRequired && <span id={`category-error-${item.id}`} role="alert">Kategori merchant belum dipelajari. Pilih kategori.</span>}</label>}{missing.transactionAt && <label>Tanggal transaksi<input name="transactionAt" type="date" required/></label>}<label>Catatan opsional<input name="note" defaultValue={item.note || ""} maxLength="1000"/></label>{item.type === "EXPENSE" && (item.merchantName || missing.merchant) && <label className="check review-check"><input name="rememberMerchant" type="checkbox"/> Ingat kategori untuk merchant ini</label>}<div className="review-actions"><button disabled={disabled}>Simpan</button><button type="button" className="secondary" disabled={disabled} onClick={() => setEditing(false)}>Batal</button></div></form>}{item.type === "EXPENSE" && <form onSubmit={assetPurchase}><label>Beli aset<select name="wealthAccountId" required defaultValue=""><option value="" disabled>Pilih akun kekayaan</option>{wealthAccounts.filter(account => account.active && account.side === "ASSET").map(account => <option key={account.id} value={account.id}>{account.name}</option>)}</select></label><button disabled={disabled}>Beli aset</button></form>}</article>;
}
