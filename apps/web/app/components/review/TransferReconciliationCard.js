import { useState } from "react";
import { dateTime, money } from "../../lib/format";
import { ProposalFacts } from "./shared";

const purposeLabels = { INTERNAL_TRANSFER: "Rekening sendiri", SAVINGS_TRANSFER: "Tabungan", INVESTMENT_CONTRIBUTION: "Investasi", ASSET_PURCHASE: "Beli aset", DEBT_PRINCIPAL_PAYMENT: "Bayar pokok utang" };
// Which akun kekayaan side/role each purpose needs (transfer_wealth_compatible).
const purposeAccounts = { SAVINGS_TRANSFER: account => account.side === "ASSET" && account.usageRole === "SAVINGS", INVESTMENT_CONTRIBUTION: account => account.side === "ASSET" && account.usageRole === "INVESTMENT", ASSET_PURCHASE: account => account.side === "ASSET", DEBT_PRINCIPAL_PAYMENT: account => account.side === "LIABILITY" };

// Merging keeps the matched transfer's own purpose; when the case proposes none,
// only an already-classified transfer can be merged into (ReconcileTransfer).
// The server marks each candidate with that same rule (candidate.mergeable), so
// a merge button is enabled only where the merge will be accepted.
export default function TransferReconciliationCard({ item, wealthAccounts = [], disabled, resolve }) {
  const canMerge = item.allowedActions?.includes("MERGE_EXISTING"); const canCreate = item.allowedActions?.includes("CONFIRM_NEW_TRANSFER");
  const choices = item.purposeChoices || [];
  const [purpose, setPurpose] = useState("");
  const accountFilter = purposeAccounts[purpose];
  const needsAccount = Boolean(accountFilter);
  const candidates = item.transferCandidates || [];
  function confirmNew(event) { event.preventDefault(); const form = new FormData(event.currentTarget); resolve("CONFIRM_NEW_TRANSFER", choices.length ? { purpose, ...(needsAccount ? { wealthAccountId: form.get("wealthAccountId") } : {}) } : {}); }
  const explanation = !canMerge && !candidates.length ? "Belum ada transfer tercatat yang cocok. Pilih tujuannya untuk mencatat transfer ini." : canCreate ? "Pilih hanya bukti yang benar-benar merupakan transfer yang sama." : "Banyak transfer mirip; hanya yang paling dekat waktunya ditampilkan. Transfer baru tidak dibuat agar tidak menggandakan catatan.";
  return <article className="review-card transfer-card"><div className="review-top"><span className="review-reason">Transfer perlu rekonsiliasi</span><strong>{money(item.amount)}</strong></div><h2>{item.description || "Transfer mungkin sudah tercatat"}</h2><ProposalFacts item={item} known={[]} missing={item.missingFacts || []}/><p>{explanation}</p>{candidates.length > 0 && <details className="review-evidence"><summary>Bukti</summary><div className="candidates">{candidates.map(candidate => <button type="button" key={candidate.id} disabled={disabled || !canMerge || candidate.mergeable !== true} onClick={() => resolve("MERGE_EXISTING", { transactionId: candidate.id })}><span>{dateTime(candidate.transactionAt)} · {candidate.description || candidate.type}</span><strong>{money(candidate.amount)}</strong></button>)}</div></details>}{canCreate ? <form onSubmit={confirmNew}>{choices.length > 0 && <label>Tujuan transfer<select name="purpose" required value={purpose} onChange={event => setPurpose(event.target.value)}><option value="" disabled>Pilih tujuan</option>{choices.map(choice => <option key={choice} value={choice}>{purposeLabels[choice] || "Lainnya"}</option>)}</select></label>}{needsAccount && <label>Akun kekayaan<select name="wealthAccountId" required defaultValue=""><option value="" disabled>Pilih akun</option>{wealthAccounts.filter(account => account.active !== false && accountFilter(account)).map(account => <option key={account.id} value={account.id}>{account.name}</option>)}</select></label>}<div className="review-actions"><button disabled={disabled}>Ini transfer baru</button><button type="button" className="danger" disabled={disabled} onClick={() => resolve("IGNORE")}>Abaikan</button></div></form> : <div className="review-actions"><button className="danger" disabled={disabled} onClick={() => resolve("IGNORE")}>Abaikan</button></div>}</article>;
}
