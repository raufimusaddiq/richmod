import Link from "next/link";
import { useState } from "react";
import { dateTime, money } from "../../lib/format";
import { ProposalFacts } from "./shared";

export default function TransferCard({ item, categories, wealthAccounts, disabled, action }) {
  const [editing, setEditing] = useState(false);
  const [transactionAt, setTransactionAt] = useState("");
  const needsDate = item.missingFacts?.includes("transaction_at");
  const blocked = disabled || (needsDate && !transactionAt);
  const incoming = item.type === "INCOME";
  function classify(values) { return action(item.id, "classify-transfer", { ...values, transactionAt: transactionAt || null }); }
  function expense(event) { event.preventDefault(); const form = new FormData(event.currentTarget); classify({ classification: "EXPENSE", categoryId: form.get("categoryId") }); }
  function assetPurchase(event) { event.preventDefault(); const form = new FormData(event.currentTarget); classify({ classification: "ASSET_PURCHASE", wealthAccountId: form.get("wealthAccountId") }); }
  if (item.type === "TRANSFER" && item.missingFacts?.includes("wealth_account")) {
    const classification = item.knownFacts?.relationship_choice;
    function replaceAccount(event) { event.preventDefault(); classify({ classification, wealthAccountId: new FormData(event.currentTarget).get("wealthAccountId") }); }
    return <article className="review-card transfer-card"><h2>Pilih akun kekayaan pengganti</h2><p>Jenis transfer tetap tersimpan. Pilih akun aktif untuk menyelesaikan tinjauan.</p><form onSubmit={replaceAccount}><label>Akun kekayaan<select name="wealthAccountId" required defaultValue=""><option value="" disabled>Pilih akun kekayaan</option>{wealthAccounts.filter(account => account.active && account.side === "ASSET" && (classification !== "INVESTMENT_ACCOUNT" || account.usageRole === "INVESTMENT")).map(account => <option key={account.id} value={account.id}>{account.name}</option>)}</select></label>{needsDate && <label>Tanggal transaksi<input type="date" required value={transactionAt} onChange={event => setTransactionAt(event.target.value)}/></label>}<button disabled={blocked}>Simpan akun pengganti</button></form><button className="danger" disabled={disabled} onClick={() => action(item.id, "reject")}>Abaikan</button></article>;
  }
  return <article className="review-card transfer-card">
    <div className="review-top"><span className="review-reason">Transfer belum dikenal</span><strong>{money(item.amount)}</strong></div>
    <h2>{item.counterparty || "Tujuan transfer belum dikenal"}</h2>
    <p>{dateTime(item.transactionAt)} · belum dihitung sebagai pengeluaran</p>
    <ProposalFacts item={item} known={[["amount_idr", money(item.amount)], ["merchant", item.counterparty]]} missing={item.missingFacts || ["transfer_relationship"]}/>
    {needsDate && <label>Tanggal transaksi<input type="date" required value={transactionAt} onChange={event => setTransactionAt(event.target.value)}/></label>}
    <p>Pilih jenisnya, atau lengkapi detail bila ini pengeluaran atau pembelian aset.</p>
    <div className="transfer-options">
      {incoming && <button disabled={blocked} onClick={() => action(item.id, "confirm", { transactionAt: transactionAt || null })}>Catat sebagai penghasilan</button>}
      <button disabled={blocked} onClick={() => classify({ classification: "OWN_ACCOUNT" })}>Rekening sendiri</button>
      <button disabled={blocked} onClick={() => classify({ classification: "HOUSEHOLD_ACCOUNT" })}>Rekening rumah tangga</button>
      {!incoming && <button disabled={blocked} onClick={() => classify({ classification: "INVESTMENT_ACCOUNT" })}>Investasi / RDN</button>}
      {!incoming && <button className="secondary" disabled={disabled} onClick={() => setEditing(true)}>Ubah detail</button>}
      <button className="danger" disabled={disabled} onClick={() => classify({ classification: "IGNORE" })}>Abaikan</button>
    </div>
    {editing && <form onSubmit={assetPurchase}><label>Beli aset<select name="wealthAccountId" required defaultValue=""><option value="" disabled>Pilih akun kekayaan</option>{wealthAccounts.filter(account => account.active && account.side === "ASSET").map(account => <option key={account.id} value={account.id}>{account.name}</option>)}</select></label><button disabled={blocked}>Beli aset</button></form>}
    {editing && <form onSubmit={expense}><label>Jika ini pengeluaran<select name="categoryId" required defaultValue=""><option value="" disabled>Pilih kategori</option>{categories.filter(category => category.active).map(category => <option key={category.id} value={category.id}>{category.name}</option>)}</select></label><div className="review-actions"><button disabled={blocked}>Catat sebagai pengeluaran</button><Link className="button secondary" href={`/transactions?id=${item.id}`}>Bukti</Link></div></form>}
  </article>;
}
