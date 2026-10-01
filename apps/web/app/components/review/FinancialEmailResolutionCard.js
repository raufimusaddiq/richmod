import { money } from "../../lib/format";
import { label } from "./shared";

// PRD §12: an entity that is already resolved is shown as known and is never
// requested again; only the unresolved dimension is asked for (PRD §13.4).
export default function FinancialEmailResolutionCard({ item, accounts, wealthAccounts, disabled, resolve }) {
  const missing = item.missingFacts;
  // A review stored before the decision contract existed has no missingFacts;
  // it still asks for both entities rather than rendering an unresolvable card.
  const needsAccount = !missing || missing.includes("funding_account") || missing.includes("account");
  const needsWealth = !missing || missing.includes("wealth_account") || missing.includes("wealthAccount");
  // The server surfaces what this review already resolved; read it from the item
  // root only. A second copy inside `decision` would be a second source of truth
  // for the same fact (PRD §3.3: never ask again what the system already knows).
  const resolvedAccountId = item.resolvedAccountId || '';
  const resolvedWealthAccountId = item.resolvedWealthAccountId || '';
  const knownAccount = needsAccount ? null : accounts.find(account => account.id === resolvedAccountId);
  const knownWealth = needsWealth ? null : wealthAccounts.find(account => account.id === resolvedWealthAccountId);
  // The canonical resolver requires both a funding account and a Akun kekayaan
  // (canonical.go), so a dimension the decision already resolved is sent back as
  // the known id instead of null. Sending null where the server requires a value
  // would 400 the moment a partial decision starts being written.
  function submit(event) { event.preventDefault(); const form = new FormData(event.currentTarget); resolve("SET_FINANCIAL_EMAIL_ENTITIES", { accountId: needsAccount ? form.get("accountId") : resolvedAccountId, wealthAccountId: needsWealth ? form.get("wealthAccountId") : resolvedWealthAccountId }); }
  return <article className="review-card"><div className="review-top"><span className="review-reason">Email finansial perlu konfirmasi</span><strong>{money(item.amount)}</strong></div><h2>{item.fundingAccountHint || item.providerAccountHint || "Rekening belum pasti"}</h2>{knownAccount && <p className="state-line"><span>Rekening sumber</span><span>{knownAccount.name} · sudah dikenali</span></p>}{knownWealth && <p className="state-line"><span>Akun kekayaan</span><span>{knownWealth.name} · sudah dikenali</span></p>}{!knownAccount && !knownWealth && <p>Pilih hanya yang belum dapat dikenali.</p>}<form onSubmit={submit}>{needsAccount && <label>Rekening sumber<select name="accountId" required defaultValue=""><option value="" disabled>Pilih rekening</option>{accounts.filter(account => account.active).map(account => <option key={account.id} value={account.id}>{account.name}</option>)}</select></label>}{needsWealth && <label>Akun kekayaan<select name="wealthAccountId" required defaultValue=""><option value="" disabled>Pilih akun kekayaan</option>{wealthAccounts.filter(account => account.active).map(account => <option key={account.id} value={account.id}>{account.name}</option>)}</select></label>}<div className="review-actions"><button disabled={disabled}>Simpan dan proses</button><button type="button" className="danger" disabled={disabled} onClick={() => resolve("IGNORE")}>Abaikan</button></div></form></article>;
}
