import Link from "next/link";
import { useState } from "react";
import { money } from "../../lib/format";
import { ProposalFacts } from "./shared";

// One household action resolves this review (ADR-036 amendment): choosing the
// account accepts the observation, which then waits on /wealth for the next
// complete snapshot. It never becomes a transaction or a partial snapshot.
export default function WealthObservationCard({ item, wealthAccounts, disabled, resolve }) {
  const [editing, setEditing] = useState(!item.resolvedWealthAccountId);
  const proposed = wealthAccounts.find(account => account.id === item.resolvedWealthAccountId);
  function choose(event) { event.preventDefault(); const account = new FormData(event.currentTarget).get("wealthAccountId"); resolve("SET_WEALTH_ACCOUNT", { wealthAccountId: account }); }
  return <article className="review-card"><div className="review-top"><span className="review-reason">Observasi kekayaan</span><strong>{money(item.amount)}</strong></div><h2>{item.institution} · {item.accountHint}</h2><ProposalFacts item={item} known={[["wealth_account", proposed?.name || item.accountHint || null]]} missing={item.missingFacts || []}/><p>Memilih akun menyelesaikan tinjauan ini. Nilainya lalu menunggu snapshot berikutnya di halaman <Link href="/wealth">Kekayaan</Link> dan tidak dicatat sebagai transaksi.</p>{!editing && proposed ? <div className="review-actions"><button disabled={disabled} onClick={() => resolve("SET_WEALTH_ACCOUNT", { wealthAccountId: proposed.id })}>Gunakan {proposed.name}</button><button className="secondary" disabled={disabled} onClick={() => setEditing(true)}>Pilih akun lain</button><button className="danger" disabled={disabled} onClick={() => resolve("IGNORE")}>Abaikan</button></div> : <form onSubmit={choose}><label>Akun kekayaan<select name="wealthAccountId" required defaultValue=""><option value="" disabled>Pilih akun</option>{wealthAccounts.filter(account => account.active !== false).map(account => <option key={account.id} value={account.id}>{account.name}</option>)}</select></label><div className="review-actions"><button disabled={disabled}>Gunakan akun ini</button>{proposed && <button type="button" className="secondary" disabled={disabled} onClick={() => setEditing(false)}>Batal</button>}<button type="button" className="danger" disabled={disabled} onClick={() => resolve("IGNORE")}>Abaikan</button></div></form>}</article>;
}
