import { Link } from "../router";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import DateInput from "./DateInput";
import { createErrorMessage, trapTarget } from "../lib/transactionForm";
import { withinCycleWindow } from "../lib/missingTransaction";
import { money } from "../lib/format";
import { documentTypeLabels, label } from "../lib/labels";
import { MAX_PHOTOS, buildQuickPayload, categoryChips, caretAfterFormat, formatRupiahInput, photoSelectionError, photoStatus, quickFormErrors, repeatLabel, sanitizeSuggestions, suggestCategory } from "../lib/quickRecord";

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';
const TYPES = [["EXPENSE", "Pengeluaran"], ["INCOME", "Pemasukan"], ["TRANSFER", "Transfer"]];
const PURPOSES = [["INTERNAL_TRANSFER", "Transfer internal"], ["SAVINGS_TRANSFER", "Pindah ke tabungan"], ["INVESTMENT_CONTRIBUTION", "Kontribusi investasi"], ["ASSET_PURCHASE", "Pembelian aset"], ["DEBT_PRINCIPAL_PAYMENT", "Pembayaran pokok utang"]];
const EMPTY_FORM = { type: "EXPENSE", amount: "", description: "", categoryId: "", accountId: "", purpose: "INTERNAL_TRANSFER", relatedWealthAccountId: "", note: "" };
const POLL_MS = 5000, MAX_POLLS = 24;

async function loadJson(url) {
  try {
    const response = await fetch(url);
    return response.ok ? await response.json() : null;
  } catch { return null; }
}

export default function QuickRecordDialog({ request, onClose, onSaved }) {
  const dialogRef = useRef(null), amountRef = useRef(null), descriptionRef = useRef(null), formRef = useRef(null);
  const missingWindow = request.missingWindow;
  const [mode, setMode] = useState(request.mode);
  const [form, setForm] = useState(EMPTY_FORM);
  const [errors, setErrors] = useState({}), [formError, setFormError] = useState(""), [saving, setSaving] = useState(false);
  const [moreCategories, setMoreCategories] = useState(false), [categoryTouched, setCategoryTouched] = useState(false);
  const [data, setData] = useState({ categories: [], accounts: [], wealthAccounts: [], suggestions: sanitizeSuggestions(null) });

  useEffect(() => {
    const dialog = dialogRef.current;
    if (dialog && !dialog.open) { dialog.showModal(); (mode === "photo" ? dialog.querySelector("input[type=file]") : amountRef.current)?.focus(); }
    return () => { if (dialog?.open) dialog.close(); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    let active = true;
    Promise.all([loadJson("/api/v1/categories"), loadJson("/api/v1/accounts"), loadJson("/api/v1/wealth/accounts"), loadJson("/api/v1/transactions/quick-suggestions")]).then(([categories, accounts, wealthAccounts, suggestions]) => {
      if (!active) return;
      const next = { categories: Array.isArray(categories) ? categories : [], accounts: Array.isArray(accounts) ? accounts : [], wealthAccounts: Array.isArray(wealthAccounts) ? wealthAccounts : [] };
      setData({ ...next, suggestions: sanitizeSuggestions(suggestions, next) });
    });
    return () => { active = false; };
  }, []);

  const activeCategories = useMemo(() => data.categories.filter(item => item.active !== false), [data.categories]);
  const activeIds = useMemo(() => new Set(activeCategories.map(item => item.id)), [activeCategories]);
  const chips = useMemo(() => categoryChips(data.suggestions.recentCategories, data.categories), [data.suggestions.recentCategories, data.categories]);
  const suggestedId = form.type === "TRANSFER" ? "" : suggestCategory(data.suggestions.descriptionMap, form.description, activeIds);
  const suggested = suggestedId && suggestedId !== form.categoryId ? activeCategories.find(item => item.id === suggestedId) : null;
  const chosenOutsideChips = form.categoryId && !chips.some(item => item.id === form.categoryId);
  const set = (patch) => setForm(current => ({ ...current, ...patch }));

  function onAmount(event) {
    const input = event.target;
    const digitsBefore = input.value.slice(0, input.selectionStart ?? input.value.length).replace(/\D/g, "").length;
    const formatted = formatRupiahInput(input.value);
    set({ amount: formatted });
    const caret = caretAfterFormat(formatted, digitsBefore);
    window.requestAnimationFrame(() => { if (document.activeElement === input) input.setSelectionRange(caret, caret); });
  }

  function onAmountKeyDown(event) {
    if (event.key !== "Enter") return;
    event.preventDefault();
    descriptionRef.current?.focus();
  }

  function repeat(item) {
    setCategoryTouched(true);
    setForm(current => ({ ...current, type: item.type, amount: formatRupiahInput(item.amount), description: item.description, categoryId: item.categoryId || "", accountId: item.accountId || "", purpose: "INTERNAL_TRANSFER", relatedWealthAccountId: "" }));
    setErrors({}); setFormError("");
    amountRef.current?.focus();
  }

  function trapTab(event) {
    if (event.key !== "Tab") return;
    const dialog = dialogRef.current, items = [...dialog.querySelectorAll(FOCUSABLE)].filter(item => item.offsetParent !== null || item === document.activeElement);
    const target = trapTarget(items, document.activeElement, event.shiftKey, dialog);
    if (target) { event.preventDefault(); target.focus(); }
  }

  async function submit(event) {
    event.preventDefault();
    if (saving) return;
    const time = new FormData(event.currentTarget).get("transactionAt") || "";
    const values = { ...form, transactionAt: time };
    const found = quickFormErrors(values, { cycle: Boolean(missingWindow) });
    setErrors(found); setFormError("");
    if (Object.keys(found).length) { setFormError("Lengkapi isian yang ditandai."); window.requestAnimationFrame(() => formRef.current?.querySelector("[aria-invalid=true]")?.focus()); return; }
    if (missingWindow && !withinCycleWindow(time, missingWindow)) { setFormError(`Pilih waktu di dalam siklus ${missingWindow.from} sampai ${missingWindow.to}.`); return; }
    setSaving(true);
    try {
      const response = await fetch("/api/v1/transactions", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(buildQuickPayload(values)) });
      const body = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(createErrorMessage(response.status, body));
      if (missingWindow) { const next = new URLSearchParams(window.location.search); next.delete("record"); window.history.replaceState(window.history.state, "", `${window.location.pathname}${next.size ? `?${next}` : ""}`); }
      onSaved(body);
    } catch (failure) {
      setFormError(failure?.name === "TypeError" ? "Koneksi terputus. Coba lagi." : failure?.message || "Koneksi terputus. Coba lagi.");
      setSaving(false);
    }
  }

  const transfer = form.type === "TRANSFER";
  const title = mode === "photo" ? "Foto bukti" : "Catat";
  return <dialog ref={dialogRef} className="create-dialog quick-record" role="dialog" aria-modal="true" aria-labelledby="quick-record-title" tabIndex={-1} onClose={onClose} onKeyDown={trapTab}>
    <div className="quick-head"><h2 id="quick-record-title">{title}</h2><button type="button" className="dialog-close" aria-label="Tutup" onClick={() => dialogRef.current?.close()}>×</button></div>
    {mode === "photo"
      ? <PhotoProof onBack={() => setMode("manual")} onClose={() => dialogRef.current?.close()}/>
      : <form ref={formRef} className="quick-form" onSubmit={submit} noValidate>
        <div className="quick-body">
          <div className="segmented" role="group" aria-label="Jenis transaksi">{TYPES.map(([key, text]) => <button key={key} type="button" aria-pressed={form.type === key} className={form.type === key ? "active" : ""} onClick={() => { set({ type: key, categoryId: key === "TRANSFER" ? "" : form.categoryId }); setErrors({}); }}>{text}</button>)}</div>
          <label className="quick-amount"><span>Jumlah (Rp) <span className="required-mark" aria-hidden="true">*</span></span>
            <input ref={amountRef} name="amount" inputMode="numeric" enterKeyHint="next" autoComplete="off" placeholder="Contoh: 25.000" value={form.amount} onChange={onAmount} onKeyDown={onAmountKeyDown} required aria-required="true" aria-invalid={errors.amount ? "true" : undefined} aria-describedby={errors.amount ? "quick-amount-error" : undefined}/>
            {errors.amount && <small id="quick-amount-error" className="field-error">{errors.amount}</small>}</label>
          {data.suggestions.frequent.length > 0 && <div className="quick-chips quick-repeat" role="group" aria-label="Ulangi cepat"><span className="quick-label">Ulangi cepat</span><div>{data.suggestions.frequent.map(item => <button key={`${item.type}-${item.amount}-${item.description}`} type="button" className="chip" title={item.description} onClick={() => repeat(item)}>{repeatLabel(item, money)}</button>)}</div></div>}
          <label><span>Deskripsi <span className="required-mark" aria-hidden="true">*</span></span>
            <input ref={descriptionRef} name="description" maxLength="500" enterKeyHint="done" autoCapitalize="sentences" autoComplete="off" value={form.description} onChange={event => { set({ description: event.target.value }); }} required aria-required="true" aria-invalid={errors.description ? "true" : undefined} aria-describedby={errors.description ? "quick-description-error" : undefined}/>
            {errors.description && <small id="quick-description-error" className="field-error">{errors.description}</small>}</label>
          {transfer
            ? <>
              <label>Tujuan transfer<select name="purpose" value={form.purpose} onChange={event => set({ purpose: event.target.value, relatedWealthAccountId: "" })}>{PURPOSES.map(([key, text]) => <option key={key} value={key}>{text}</option>)}</select></label>
              {form.purpose !== "INTERNAL_TRANSFER" && <label>Akun kekayaan terkait<select name="relatedWealthAccountId" value={form.relatedWealthAccountId} required aria-invalid={errors.relatedWealthAccountId ? "true" : undefined} onChange={event => set({ relatedWealthAccountId: event.target.value })}><option value="">Pilih akun kekayaan</option>{data.wealthAccounts.filter(item => item.active !== false).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select>{errors.relatedWealthAccountId && <small className="field-error">{errors.relatedWealthAccountId}</small>}</label>}
              <label>Akun sumber<select name="accountId" value={form.accountId} required aria-invalid={errors.accountId ? "true" : undefined} onChange={event => set({ accountId: event.target.value })}><option value="">Pilih akun sumber</option>{data.accounts.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select>{errors.accountId && <small className="field-error">{errors.accountId}</small>}</label>
            </>
            : <div className="quick-chips" role="group" aria-label="Kategori"><span className="quick-label">Kategori</span>
              <div>
                {chips.map(item => <button key={item.id} type="button" className="chip" aria-pressed={form.categoryId === item.id} onClick={() => { setCategoryTouched(true); set({ categoryId: form.categoryId === item.id ? "" : item.id }); }}>{item.name}</button>)}
                {chosenOutsideChips && <button type="button" className="chip" aria-pressed="true" onClick={() => set({ categoryId: "" })}>{activeCategories.find(item => item.id === form.categoryId)?.name || "Kategori"}</button>}
                <button type="button" className="chip chip-more" aria-expanded={moreCategories} onClick={() => setMoreCategories(open => !open)}>Lainnya…</button>
              </div>
              {suggested && !categoryTouched && <div className="quick-suggest" role="status"><span>Saran kategori:</span><button type="button" className="chip chip-suggest" onClick={() => { setCategoryTouched(true); set({ categoryId: suggested.id }); }}>{suggested.name}</button></div>}
              {moreCategories && <label className="quick-other"><span>Semua kategori</span><select name="categoryId" value={form.categoryId} onChange={event => { setCategoryTouched(true); set({ categoryId: event.target.value }); }}><option value="">Tanpa kategori</option>{activeCategories.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>}
            </div>}
          {missingWindow && <p className="notice">Catat transaksi yang terlewat di siklus {missingWindow.from} sampai {missingWindow.to}. Waktunya dibatasi ke siklus itu agar sisa siklusnya dihitung ulang.</p>}
          {missingWindow
            ? <label>Waktu<DateInput name="transactionAt" type="datetime-local" required min={missingWindow.min} max={missingWindow.max} defaultValue={missingWindow.value} aria-invalid={errors.transactionAt ? "true" : undefined}/></label>
            : <details className="quick-more"><summary>Waktu, akun, dan catatan</summary>
              <div className="quick-more-fields">
                <label>Waktu<DateInput name="transactionAt" type="datetime-local"/></label>
                {!transfer && <label>Akun<select name="accountId" value={form.accountId} onChange={event => set({ accountId: event.target.value })}><option value="">Tanpa akun</option>{data.accounts.filter(item => item.active).map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>}
                <label>Catatan<textarea name="note" autoCapitalize="sentences" value={form.note} onChange={event => set({ note: event.target.value })}/></label>
              </div></details>}
          {formError && <p role="alert" className="error-notice">{formError}</p>}
        </div>
        <div className="quick-savebar"><button type="button" className="secondary" onClick={() => setMode("photo")}>Foto bukti</button><button type="submit" disabled={saving}>{saving ? "Menyimpan…" : "Catat"}</button></div>
      </form>}
  </dialog>;
}

function PhotoProof({ onBack, onClose }) {
  const [files, setFiles] = useState([]), [error, setError] = useState(""), [working, setWorking] = useState(false), [notice, setNotice] = useState("");
  const [recent, setRecent] = useState(null), [retrying, setRetrying] = useState("");
  const polls = useRef(0), timer = useRef(0);

  const refresh = useCallback(async () => {
    const list = await loadJson("/api/v1/documents");
    if (Array.isArray(list)) setRecent(list.slice(0, 5));
    return Array.isArray(list) ? list.slice(0, 5) : null;
  }, []);
  const poll = useCallback(async () => {
    window.clearTimeout(timer.current);
    const list = await refresh();
    const busy = list?.some(item => photoStatus(item).tone === "pending");
    if (busy && polls.current < MAX_POLLS) { polls.current += 1; timer.current = window.setTimeout(poll, POLL_MS); }
  }, [refresh]);
  useEffect(() => { poll(); return () => window.clearTimeout(timer.current); }, [poll]);

  async function upload(event) {
    event.preventDefault();
    if (working) return;
    const problem = photoSelectionError(files);
    if (problem) { setError(problem); return; }
    setWorking(true); setError(""); setNotice("");
    try {
      const body = new FormData();
      for (const file of files) body.append("file", file);
      const response = await fetch("/api/v1/documents", { method: "POST", body });
      if (!response.ok) { const failure = await response.json().catch(() => ({})); setError(failure.error || "Foto belum dapat diunggah."); return; }
      setFiles([]); event.currentTarget.reset?.(); setNotice("Foto diterima dan sedang diproses.");
      polls.current = 0; await poll();
    } catch { setError("Koneksi terputus saat mengunggah. Coba lagi."); }
    finally { setWorking(false); }
  }

  async function retry(item) {
    if (retrying) return;
    setRetrying(item.id); setError("");
    try {
      const response = await fetch(`/api/v1/documents/${item.id}/reprocess`, { method: "POST" });
      if (!response.ok) { setError(response.status === 409 ? "Hanya foto yang gagal yang dapat diproses ulang." : "Foto belum dapat diproses ulang."); return; }
      polls.current = 0; await poll();
    } catch { setError("Koneksi terputus. Coba proses ulang lagi."); }
    finally { setRetrying(""); }
  }

  const info = files.length ? `${files.length} gambar · ${(files.reduce((sum, file) => sum + file.size, 0) / 1024 / 1024).toFixed(1)} MB total` : "Belum ada gambar dipilih";
  return <form className="quick-form" onSubmit={upload} noValidate>
    <div className="quick-body">
      <label className="file-picker"><span>Pilih gambar</span><span className="file-picker-button" aria-hidden="true">{files.length ? "Ganti gambar" : "Pilih berkas"}</span><input name="file" type="file" multiple accept="image/jpeg,image/png,.jpg,.jpeg,.png" onChange={event => { setFiles([...event.target.files]); setError(""); }}/></label>
      <small>JPG atau PNG · maksimal {MAX_PHOTOS} gambar · maksimal 10 MB total</small>
      <small aria-live="polite">{info}</small>
      {error && <p role="alert" className="error-notice">{error}</p>}
      {notice && <p role="status" className="notice">{notice}</p>}
      <div className="quick-recent"><span className="quick-label">Unggahan terbaru</span>
        {recent === null ? <p className="muted">Memuat…</p> : !recent.length ? <p className="muted">Belum ada foto bukti.</p> : <ul>{recent.map(item => { const status = photoStatus(item); return <li key={item.id} data-tone={status.tone}><span>{label(documentTypeLabels, item.documentType) || "Foto bukti"}</span><b>{status.text}</b>{status.retry && <button type="button" className="secondary" disabled={Boolean(retrying)} onClick={() => retry(item)}>{retrying === item.id ? "Memproses…" : "Proses ulang"}</button>}</li>; })}</ul>}
        <Link href="/documents" onClick={onClose}>Lihat semua riwayat foto bukti ›</Link>
      </div>
    </div>
    <div className="quick-savebar"><button type="button" className="secondary" onClick={onBack}>Catat manual</button><button type="submit" disabled={working || !files.length}>{working ? "Mengunggah…" : "Unggah"}</button></div>
  </form>;
}
