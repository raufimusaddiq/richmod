import { useEffect, useRef, useState } from "react";
import DateInput from "../components/DateInput";
import AppShell from "../components/AppShell";
import Presence from "../components/Presence";
import { ErrorNotice, Skeleton } from "../components/Feedback";
import TransactionList from "../components/TransactionList";
import { useCategoryPalette } from "../components/CategoryPaletteProvider";
import useAuth from "../components/useAuth";
import useDialogs from "../components/useDialogs";
import useDrawerA11y from "../components/useDrawerA11y";
import { LoadError, useDrawerRecord, useGuardedLoad } from "../lib/useLoadState";
import { dateTime, money, statusLabel, typeLabel } from "../lib/format";
import { hidesVoided, originTimeline, periodRange, periodValue, transactionDisplayName, withVoidedPolicy } from "../lib/transactionView";
import { jakartaDay } from "../lib/ledgerDays";
import { selectionHref } from "../lib/cycleReview";
import { cycleEntryWindow } from "../lib/missingTransaction";
import { useQuickRecord } from "../components/QuickRecordProvider";
import { evidenceTypeText } from "../lib/labels";
import { MARK_SALARY_CONFIRM_MESSAGE, MARK_SALARY_SUCCESS_MESSAGE, VOID_CONFIRM_MESSAGE, canMarkSalary, markSalaryFailure, REVERSE_MERGE_SUCCESS_MESSAGE, VOID_SUCCESS_MESSAGE, canEditTransaction, canVoidTransaction, editErrorMessage, editFormErrors, editInitialValues, editOptionsReady, editPayload, editSelectOptions, editableFields, dateRangeError, hasActiveFilters, isScopedToTransaction, listSearch, normalizeAmount, mergeSummary, pageLabel, pageSize, resultCountLabel, reverseMergeConfirmMessage, reverseMergeFailure, transactionsErrorMessage, trapTarget, voidFailure } from "../lib/transactionForm";

const PAGE_SIZE = 50;
const focusable = "a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex='-1'])";


export default function TransactionsPage() {
  const user = useAuth();
  const palette = useCategoryPalette();
  const [items, setItems] = useState([]);
  const [categories, setCategories] = useState([]);
  const [accounts, setAccounts] = useState([]);
  const [categoriesLoaded, setCategoriesLoaded] = useState(false);
  const [accountsLoaded, setAccountsLoaded] = useState(false);
  const [wealthAccounts, setWealthAccounts] = useState([]);
  const [members, setMembers] = useState([]);
  const drawer = useDrawerRecord(async (id, signal) => {
    const [transaction, evidenceResponse, auditResponse] = await Promise.all([fetch(`/api/v1/transactions/${id}`, { signal }), fetch(`/api/v1/transactions/${id}/evidence`, { signal }), fetch(`/api/v1/transactions/${id}/audit`, { signal })]);
    if (!transaction.ok || !evidenceResponse.ok || !auditResponse.ok) throw new LoadError("Detail transaksi belum dapat dimuat. Coba lagi.");
    return { transaction: await transaction.json(), evidence: await evidenceResponse.json(), audit: await auditResponse.json() };
  }, "Detail transaksi belum dapat dimuat. Coba lagi.");
  const closeDetail = () => {
    if (typeof window !== "undefined" && new URLSearchParams(window.location.search).has("open")) window.history.replaceState(window.history.state, "", drawerUrl(""));
    drawer.close();
  };
  const drawerRef = useDrawerA11y(drawer.status !== "closed", closeDetail);
  const selected = drawer.data?.transaction || (drawer.id ? { id: drawer.id } : null);
  const timeline = originTimeline(drawer.data?.evidence || [], drawer.data?.audit || []);
  const [nextCursor, setNextCursor] = useState("");
  const [cursorHistory, setCursorHistory] = useState([]);
  const [total, setTotal] = useState(null);
  const [voidedHidden, setVoidedHidden] = useState(0), [cycleWindow, setCycleWindow] = useState(null);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [createdId, setCreatedId] = useState(""), [refreshToken, setRefreshToken] = useState(0);
  const consumedRefresh = useRef(0);
  const refreshPending = refreshToken !== consumedRefresh.current;
  const [filterError, setFilterError] = useState(""), [filterKey, setFilterKey] = useState(0);
  const { open: openQuickRecord } = useQuickRecord();
  const openCreate = (cycleWindow = null) => openQuickRecord({ missingWindow: cycleWindow });
  const editDialog = useRef(null), editOpener = useRef(null), editStale = useRef(false);
  const [editValues, setEditValues] = useState(() => editInitialValues());
  const [editTouched, setEditTouched] = useState(() => new Set());
  const [editError, setEditError] = useState(""), [editing, setEditing] = useState(false), [editFieldErrors, setEditFieldErrors] = useState({});
  const editReady = editOptionsReady(selected, { categoriesLoaded, accountsLoaded });
  function onEditFieldChange(name, value) {
    setEditValues(current => ({ ...current, [name]: value }));
    setEditTouched(current => { if (current.has(name)) return current; const next = new Set(current); next.add(name); return next; });
  }
  function openEdit() {
    if (!selected?.id || !editOptionsReady(selected, { categoriesLoaded, accountsLoaded })) return;
    setEditValues(editInitialValues(selected)); setEditTouched(new Set());
    setEditError(""); setEditFieldErrors({}); editStale.current = false; editOpener.current = document.activeElement;
    const dialog = editDialog.current; if (!dialog || dialog.open) return;
    dialog.showModal(); (dialog.querySelector("select, input") || dialog).focus();
  }
  function closeEdit() { editDialog.current?.close(); }
  function onEditClosed() {
    setEditError(""); setEditFieldErrors({}); setEditTouched(new Set());
    const opener = editOpener.current; editOpener.current = null;
    if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
    if (editStale.current && drawer.id) { editStale.current = false; drawer.open(drawer.id); }
  }
  function onEditKeyDown(event) {
    event.stopPropagation();
    if (event.key !== "Tab") return;
    const dialog = editDialog.current, items = [...dialog.querySelectorAll(focusable)];
    const target = trapTarget(items, document.activeElement, event.shiftKey, dialog);
    if (target) { event.preventDefault(); target.focus(); }
  }
  async function saveEdit(event) {
    event.preventDefault(); if (editing || !selected?.id) return;
    const formEl = event.currentTarget;
    const errors = editFormErrors(editValues, selected); setEditFieldErrors(errors);
    if (Object.keys(errors).length) { setEditError("Lengkapi isian yang ditandai."); formEl.querySelector("[aria-invalid=true]")?.focus(); return; }
    setEditing(true); setEditError("");
    try {
      const payload = editPayload(selected, editValues, editTouched);
      if (!Object.keys(payload).length) { closeEdit(); return; }
      const response = await fetch(`/api/v1/transactions/${selected.id}`, { method: "PATCH", headers: { "Content-Type": "application/json", "If-Match": selected.updatedAt }, body: JSON.stringify(payload) });
      const body = await response.json().catch(() => ({}));
      if (!response.ok) { if (body?.code === "STALE_EDIT") editStale.current = true; throw new Error(editErrorMessage(response.status, body)); }
      closeEdit(); drawer.open(selected.id); load();
    } catch (err) { setEditError(err.name === "TypeError" ? "Koneksi terputus. Coba lagi." : err.message || "Koneksi terputus. Coba lagi."); }
    finally { setEditing(false); }
  }

  const { confirm, dialogs } = useDialogs();
  const actionPending = useRef(false), openId = useRef(null);
  const [busyAction, setBusyAction] = useState(""), [actionNotice, setActionNotice] = useState(null);
  useEffect(() => { openId.current = drawer.id; setActionNotice(null); }, [drawer.id]);
  async function runDrawerAction({ key, confirmText, confirmLabel, danger = true, url, success, failure }) {
    const id = selected?.id;
    if (actionPending.current || !id) return;
    actionPending.current = true;
    try {
      if (!(await confirm(confirmText, { confirmLabel, danger }))) return;
      setBusyAction(key); setActionNotice(null);
      let outcome;
      try {
        const response = await fetch(url, { method: "POST" });
        outcome = response.ok ? { kind: "success", message: success.message, inbox: success.inbox } : failure(response.status, await response.json().catch(() => ({})));
      } catch { outcome = { kind: "error", message: "Koneksi terputus. Periksa status transaksi sebelum mencoba lagi.", reload: true }; }
      if (openId.current === id) { setActionNotice(outcome); if (outcome.kind === "success" || outcome.reload) drawer.open(id); }
      if (outcome.kind === "success" || outcome.reload) load();
    } finally { actionPending.current = false; setBusyAction(""); }
  }
  const voidTransaction = () => canVoidTransaction(selected) && runDrawerAction({ key: "void", confirmText: VOID_CONFIRM_MESSAGE, confirmLabel: "Ya, batalkan", url: `/api/v1/transactions/${selected.id}/void`, success: { message: VOID_SUCCESS_MESSAGE }, failure: voidFailure });
  const markSalary = () => canMarkSalary(selected) && runDrawerAction({ key: "salary", confirmText: MARK_SALARY_CONFIRM_MESSAGE, confirmLabel: "Ya, tandai", danger: false, url: `/api/v1/transactions/${selected.id}/mark-salary`, success: { message: MARK_SALARY_SUCCESS_MESSAGE }, failure: markSalaryFailure });
  const reverseMerge = merge => runDrawerAction({ key: merge.id, confirmText: reverseMergeConfirmMessage(merge, money), confirmLabel: "Ya, pisahkan", url: `/api/v1/reconciliation-merges/${merge.id}/reverse`, success: { message: REVERSE_MERGE_SUCCESS_MESSAGE, inbox: true }, failure: reverseMergeFailure });

  const loader = useGuardedLoad(async ({ signal, current }, search = window.location.search) => {
    const requestSearch = withVoidedPolicy(listSearch(search));
    const [transactions, categoryResponse, accountResponse, wealthAccountResponse, memberResponse, cycleResponse] = await Promise.all([fetch(`/api/v1/transactions${requestSearch}`, { signal }), fetch("/api/v1/categories", { signal }), fetch("/api/v1/accounts", { signal }), fetch("/api/v1/wealth/accounts", { signal }), fetch("/api/v1/household/members", { signal }), fetch("/api/v1/analytics/cycle", { signal })]);
    const optional = async response => (response.ok ? response.json().catch(() => null) : null);
    const [categoryList, accountList, wealthList, memberList, cycleInfo] = [await optional(categoryResponse), await optional(accountResponse), await optional(wealthAccountResponse), await optional(memberResponse), await optional(cycleResponse)];
    if (current()) {
      if (Array.isArray(categoryList)) { setCategories(categoryList); setCategoriesLoaded(true); }
      if (Array.isArray(accountList)) { setAccounts(accountList); setAccountsLoaded(true); }
      if (Array.isArray(wealthList)) setWealthAccounts(wealthList);
      if (Array.isArray(memberList)) setMembers(memberList);
    }
    if (!transactions.ok) { const body = await transactions.json().catch(() => ({})); throw new LoadError(transactionsErrorMessage(transactions.status, body)); }
    const list = await transactions.json(), cursor = transactions.headers.get("X-Next-Cursor") || "";
    const reportedTotal = Number.parseInt(transactions.headers.get("X-Total-Count") ?? "", 10);
    const reportedVoided = Number.parseInt(transactions.headers.get("X-Voided-Count") ?? "", 10);
    if (!current()) return;
    setItems(Array.isArray(list) ? list : []); setNextCursor(cursor);
    setTotal(Number.isFinite(reportedTotal) ? reportedTotal : null);
    setVoidedHidden(Number.isFinite(reportedVoided) ? reportedVoided : 0);
    if (cycleInfo?.kind === "CURRENT_CYCLE" && cycleInfo.start) setCycleWindow({ start: cycleInfo.start });
  }, { fallback: "Koneksi terputus saat memuat riwayat transaksi. Coba lagi.", onFailure: () => setNextCursor("") });
  const load = loader.run;
  const loading = loader.loading || loader.refreshing || (!loader.hasLoaded && !loader.failed);
  const error = loader.error;

  useEffect(() => {
    if (!user) return;
    const state = { ...window.history.state, cursorHistory: window.history.state?.cursorHistory || [] };
    setCursorHistory(state.cursorHistory); window.history.replaceState(state, "", window.location.href); load();
  }, [user, load]);
  useEffect(() => {
    const onPopState = event => {
      setCursorHistory(event.state?.cursorHistory || []);
      const params = new URLSearchParams(window.location.search);
      const id = params.get("open") || params.get("id");
      if (id) drawer.open(id); else drawer.close();
      load();
    };
    window.addEventListener("popstate", onPopState); return () => window.removeEventListener("popstate", onPopState);
  }, [load, drawer.open, drawer.close]);
  useEffect(() => {
    if (!loading && loader.hasLoaded && refreshPending) consumedRefresh.current = refreshToken;
  }, [loading, loader.hasLoaded, refreshPending, refreshToken]);
  useEffect(() => { if (user) { const params = new URLSearchParams(window.location.search); const id = params.get("open") || params.get("id"); if (id) drawer.open(id); } }, [user, drawer.open]);
  useEffect(() => { if (!user) return; const window_ = cycleEntryWindow(window.location.search); if (window_) openCreate(window_); }, [user, openQuickRecord]);
  useEffect(() => {
    if (!user) return undefined;
    const refresh = event => { setCreatedId(event.detail?.voided ? "" : event.detail?.id || ""); load(); };
    window.addEventListener("richmod:transaction-created", refresh);
    return () => window.removeEventListener("richmod:transaction-created", refresh);
  }, [user, load]);
  useEffect(() => {
    if (!user || drawer.status !== "ready" || !canEditTransaction(selected) || loading) return undefined;
    if (editOptionsReady(selected, { categoriesLoaded, accountsLoaded })) return undefined;
    const controller = new AbortController(), fields = editableFields(selected);
    const fetchList = url => fetch(url, { signal: controller.signal }).then(response => (response.ok ? response.json().catch(() => null) : null)).catch(() => null);
    Promise.all([
      fields.category && !categoriesLoaded ? fetchList("/api/v1/categories") : Promise.resolve(null),
      fields.account && !accountsLoaded ? fetchList("/api/v1/accounts") : Promise.resolve(null),
    ]).then(([categoryList, accountList]) => {
      if (controller.signal.aborted) return;
      if (Array.isArray(categoryList)) { setCategories(categoryList); setCategoriesLoaded(true); }
      if (Array.isArray(accountList)) { setAccounts(accountList); setAccountsLoaded(true); }
    });
    return () => controller.abort();
  }, [user, drawer.status, drawer.id, selected, loading, categoriesLoaded, accountsLoaded]);

  async function filter(event) {
    event.preventDefault(); const form = new FormData(event.currentTarget); const query = new URLSearchParams();
    for (const [key, value] of form.entries()) if (value) query.set(key, value);
    const rangeError = dateRangeError(form.get("from"), form.get("to"));
    if (rangeError) { setFilterError(rangeError); return; }
    setFilterError("");
    const previous = new URLSearchParams(window.location.search);
    if (previous.has("cycle")) query.set("cycle", previous.get("cycle"));
    if (previous.has("review")) query.set("review", previous.get("review"));
    if (previous.has("merchantId")) query.set("merchantId", previous.get("merchantId"));
    if (previous.get("showVoided") === "1") query.set("showVoided", "1");
    query.set("limit", String(PAGE_SIZE)); setCursorHistory([]); setCreatedId(""); setRefreshToken(token => token + 1); setFiltersOpen(false);
    const search = query.toString() ? `?${query}` : ""; window.history.replaceState({ ...window.history.state, cursorHistory: [] }, "", `/transactions${search}`); await load(search);
  }

  function resetFilters() {
    setFilterError(""); setFilterKey(key => key + 1); setCursorHistory([]); setCreatedId(""); setRefreshToken(token => token + 1);
    drawer.close();
    window.history.replaceState({ ...window.history.state, cursorHistory: [] }, "", "/transactions"); load("");
  }

  async function nextPage() {
    if (!nextCursor) return;
    const query = new URLSearchParams(window.location.search); const history = [...cursorHistory, query.get("cursor") || ""]; setCursorHistory(history); query.set("cursor", nextCursor); query.set("limit", String(pageSize(window.location.search))); query.delete("open"); drawer.close();
    const search = `?${query}`; window.history.pushState({ ...window.history.state, cursorHistory: history }, "", `/transactions${search}`); await load(search);
  }

  async function previousPage() {
    if (!cursorHistory.length) return;
    const history = cursorHistory.slice(0, -1), previous = cursorHistory[cursorHistory.length - 1];
    const query = new URLSearchParams(window.location.search);
    if (previous) query.set("cursor", previous); else query.delete("cursor");
    query.set("limit", String(pageSize(window.location.search))); query.delete("open");
    setCursorHistory(history); drawer.close();
    const search = `?${query}`; window.history.replaceState({ ...window.history.state, cursorHistory: history }, "", `/transactions${search}`); await load(search);
  }

  async function setShowVoided(show) {
    const next = new URLSearchParams(window.location.search);
    if (show) next.set("showVoided", "1"); else next.delete("showVoided");
    next.delete("cursor"); next.delete("open"); setCursorHistory([]); drawer.close(); setRefreshToken(token => token + 1);
    const search = next.toString() ? `?${next}` : "";
    window.history.replaceState({ ...window.history.state, cursorHistory: [] }, "", `/transactions${search}`); await load(search);
  }
  async function applyPeriod(value) {
    const range = periodRange(value, { cycle: cycleWindow, today: jakartaDay(new Date()) });
    const next = new URLSearchParams(window.location.search);
    for (const [key, text] of [["from", range.from], ["to", range.to]]) { if (text) next.set(key, text); else next.delete(key); }
    next.delete("cursor"); next.delete("open"); setCursorHistory([]); drawer.close(); setCreatedId(""); setRefreshToken(token => token + 1); setFilterKey(key => key + 1);
    const search = next.toString() ? `?${next}` : "";
    window.history.replaceState({ ...window.history.state, cursorHistory: [] }, "", `/transactions${search}`); await load(search);
  }

  const openDetail = item => { window.history.pushState(window.history.state, "", drawerUrl(item.id)); drawer.open(item.id); };

  if (user === null) return <main className="loading" role="status" aria-live="polite">Memuat…</main>;
  if (!user) return null;
  const scoped = isScopedToTransaction(typeof window === "undefined" ? "" : window.location.search);
  const query = new URLSearchParams(typeof window === "undefined" ? "" : window.location.search);
  const moreCount = ["type", "status", "source", "from", "to"].filter(key => query.get(key)).length;
  const period = periodValue(`?${query}`, { cycle: cycleWindow, today: jakartaDay(new Date()) });
  const voidedShown = !hidesVoided(`?${query}`) && !query.get("status");
  const size = pageSize(typeof window === "undefined" ? "" : window.location.search, PAGE_SIZE);
  const categoryColors = palette;
  return <AppShell user={user} eyebrow="Transaksi" title="Riwayat Transaksi" actions={<><button onClick={() => openCreate()}>Tambah transaksi</button><span className="header-meta">{PAGE_SIZE} per halaman · Asia/Jakarta</span></>}>
    {query.get("cycle") && <p className="notice"><a href={selectionHref({ view: "cycle", cycle: query.get("cycle"), category: query.get("categoryId") || "", step: query.get("review") || "" })}>Kembali ke tinjauan siklus</a> · {query.get("from")} sampai {query.get("to")}</p>}
    {query.get("merchantId") && <div className="notice feedback"><span>Filter merchant terpilih dari tinjauan siklus.</span><button className="secondary" type="button" onClick={() => { const next = new URLSearchParams(window.location.search); next.delete("merchantId"); next.delete("cursor"); window.history.replaceState({ ...window.history.state, cursorHistory: [] }, "", `/transactions?${next}`); setCursorHistory([]); load(`?${next}`); }}>Hapus filter merchant</button></div>}
    <form id="transaction-filters" key={filterKey} className="filter-bar" onSubmit={filter} inert={loading}>
      <div className="filter-primary">
        <label className="filter-search"><span>Cari</span><input name="q" type="search" enterKeyHint="search" placeholder="Cari tempat transaksi atau catatan" defaultValue={query.get("q") || ""}/></label>
        <label className="filter-chip"><span>Periode</span><select value={period} onChange={event => applyPeriod(event.target.value)}><option value="all">Semua waktu</option><option value="cycle" disabled={!cycleWindow}>{cycleWindow ? "Siklus ini" : "Siklus ini (belum diatur)"}</option><option value="7d">7 hari terakhir</option><option value="30d">30 hari terakhir</option>{period === "custom" && <option value="custom">Rentang kustom</option>}</select></label>
        <label className="filter-chip"><span>Kategori</span><select name="categoryId" defaultValue={query.get("categoryId") || ""} onChange={event => event.currentTarget.form.requestSubmit()}><option value="">Semua</option><option value="uncategorized">Belum dikategorikan</option>{categories.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
        <label className="filter-chip"><span>Anggota</span><select name="memberId" defaultValue={query.get("memberId") || ""} onChange={event => event.currentTarget.form.requestSubmit()}><option value="">Semua</option>{members.map(item => <option key={item.id} value={item.id}>{item.displayName}</option>)}</select></label>
        <label className="filter-chip"><span>Akun</span><select name="accountId" defaultValue={query.get("accountId") || ""} onChange={event => event.currentTarget.form.requestSubmit()}><option value="">Semua</option>{accounts.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>
        <button type="button" className="secondary filter-toggle" aria-expanded={filtersOpen} aria-controls="transaction-filters-more" onClick={() => setFiltersOpen(open => !open)}>Filter lain{moreCount ? ` (${moreCount})` : ""}</button>
        {hasActiveFilters(`?${query}`) && <button type="button" className="secondary" onClick={resetFilters}>Hapus filter</button>}
      </div>
      <div id="transaction-filters-more" className="filter-more" hidden={!filtersOpen}>
        <label className="filter-field"><span>Tipe</span><select name="type" defaultValue={query.get("type") || ""}><option value="">Semua tipe</option><option value="SPENDING">Pengeluaran & refund</option>{Object.entries(typeLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
        <label className="filter-field"><span>Status</span><select name="status" defaultValue={query.get("status") || ""}><option value="">Semua status</option>{Object.entries(statusLabel).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
        <label className="filter-field"><span>Sumber</span><select name="source" defaultValue={query.get("source") || ""}><option value="">Semua sumber</option><option value="BANK_EMAIL">Email bank</option><option value="TELEGRAM_TEXT">Pesan Telegram</option><option value="TELEGRAM_IMAGE">Gambar Telegram</option><option value="WEB_MANUAL">Input web</option><option value="WEB_IMAGE">Dokumen web</option></select></label>
        <label className="filter-field filter-date"><span>Dari tanggal</span><DateInput name="from" type="date" defaultValue={query.get("from") || ""}/></label>
        <label className="filter-field filter-date"><span>Sampai tanggal</span><DateInput name="to" type="date" defaultValue={query.get("to") || ""}/></label>
        <button>Terapkan filter</button>
      </div>
      <input name="merchantId" type="hidden" defaultValue={query.get("merchantId") || ""}/>
    </form>
    {filterError && <p role="alert" className="error-notice">{filterError}</p>}
    <ErrorNotice message={error} retry={() => load()}/>
    {!loader.hasLoaded && !loader.failed ? <Skeleton cards={1} rows={6}/> : !loader.hasLoaded ? null : <><section className="surface ledger-panel" aria-busy={loading} data-refreshed={(refreshPending && !loading) || undefined}><div className="section-title"><div><span className="eyebrow">Hasil</span><div role="status" aria-live="polite"><h2>{resultCountLabel(total, items.length, cursorHistory.length * size)}</h2></div>{(voidedHidden > 0 || voidedShown) && !scoped && <p className="voided-note">{voidedShown ? <><span>Transaksi dibatalkan ikut ditampilkan · </span><button type="button" className="link-button" onClick={() => setShowVoided(false)}>Sembunyikan</button></> : <><span>{voidedHidden} dibatalkan disembunyikan · </span><button type="button" className="link-button" onClick={() => setShowVoided(true)}>Tampilkan</button></>}</p>}{scoped && <p className="notice">Menampilkan hanya transaksi dari tautan bukti. <a href="/transactions" onClick={event => { event.preventDefault(); resetFilters(); }}>Lihat semua transaksi</a></p>}</div></div><TransactionList items={items} onSelect={openDetail} newId={createdId} hasFilters={hasActiveFilters(`?${query}`)} onClearFilters={resetFilters} categoryColors={categoryColors}/></section>{!scoped && <div className="pagination-actions"><button className="secondary" disabled={loading || !cursorHistory.length} onClick={previousPage}>Sebelumnya</button><span className="pagination-status">{pageLabel(cursorHistory.length * size, items.length, size)}</span><button className="secondary" disabled={loading || !nextCursor} onClick={nextPage}>Berikutnya</button></div>}</>}
    {drawer.status === "ready" && canEditTransaction(selected) && editReady && (() => { const fields = editableFields(selected); const categoryOptions = editSelectOptions(categories, selected.categoryId, selected.categoryName); const accountOptions = editSelectOptions(accounts, selected.accountId, selected.accountName); return <dialog ref={editDialog} className="create-dialog" role="dialog" aria-modal="true" aria-labelledby="edit-transaction-title" tabIndex={-1} onClose={onEditClosed} onKeyDown={onEditKeyDown}><button type="button" className="dialog-close" aria-label="Tutup" onClick={closeEdit}>×</button><form key={`${selected.id}-${selected.updatedAt}`} className="transaction-form" onSubmit={saveEdit} noValidate><div className="section-title"><h2 id="edit-transaction-title">Ubah transaksi</h2></div><div className="dialog-fields"><p className="notice">Jumlah, waktu, dan jenis tidak bisa diubah. Batalkan lalu catat ulang jika salah.</p>{fields.category && <label>Kategori<select name="categoryId" value={editValues.categoryId} onChange={event => onEditFieldChange("categoryId", event.target.value)}><option value="">Tanpa kategori</option>{categoryOptions.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>}{fields.account && <label>Akun<select name="accountId" value={editValues.accountId} onChange={event => onEditFieldChange("accountId", event.target.value)}><option value="">Tanpa akun</option>{accountOptions.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label>}{fields.place && <label>Tempat<input name="merchantName" maxLength="160" autoCapitalize="words" autoCorrect="off" enterKeyHint="next" value={editValues.merchantName} onChange={event => onEditFieldChange("merchantName", event.target.value)} aria-invalid={editFieldErrors.merchantName ? "true" : undefined} aria-describedby="edit-place-help"/><small id="edit-place-help" className="field-hint">Transaksi berikutnya dari nama ini ikut berubah.{selected.counterpartyName && selected.counterpartyName !== selected.merchantName ? ` Dari bank: ${selected.counterpartyName}.` : ""}</small></label>}<label><span>Deskripsi{selected.description && <span className="required-mark" aria-hidden="true"> *</span>}</span><input name="description" maxLength="500" autoCapitalize="sentences" enterKeyHint="done" required={Boolean(selected.description)} aria-required={selected.description ? "true" : undefined} value={editValues.description} onChange={event => onEditFieldChange("description", event.target.value)} aria-invalid={editFieldErrors.description ? "true" : undefined} aria-describedby={editFieldErrors.description ? "edit-description-error" : undefined}/>{editFieldErrors.description && <small id="edit-description-error" className="field-error">{editFieldErrors.description}</small>}</label><label>Catatan<textarea name="note" maxLength="2000" autoCapitalize="sentences" value={editValues.note} onChange={event => onEditFieldChange("note", event.target.value)} aria-invalid={editFieldErrors.note ? "true" : undefined} aria-describedby={editFieldErrors.note ? "edit-note-error" : undefined}/>{editFieldErrors.note && <small id="edit-note-error" className="field-error">{editFieldErrors.note}</small>}</label>{editError && <p role="alert" className="error-notice">{editError}</p>}</div><div className="dialog-actions"><button type="button" onClick={closeEdit}>Batal</button><button disabled={editing}>{editing ? "Menyimpan…" : "Simpan"}</button></div></form></dialog>; })()}
    {dialogs}
    <Presence show={drawer.status !== "closed"}>{() => <div className="drawer-backdrop" onClick={closeDetail}><aside ref={drawerRef} tabIndex={-1} className="detail-drawer" role="dialog" aria-modal="true" aria-label="Detail transaksi" aria-busy={drawer.status === "loading"} onClick={event => event.stopPropagation()}><button className="drawer-close" aria-label="Tutup detail" onClick={closeDetail}>×</button>{drawer.status === "loading" ? <p>Memuat detail…</p> : drawer.status === "error" ? <div><p role="alert">{drawer.error}</p><button onClick={drawer.retry}>Coba lagi</button></div> : <><span className="eyebrow">Detail transaksi</span><h2>{transactionDisplayName(selected)}</h2>{selected.merchantName && selected.counterpartyName && selected.counterpartyName !== selected.merchantName && <p className="detail-source">Dari bank: {selected.counterpartyName}</p>}<strong className="detail-amount">{money(selected.amount)}</strong><dl style={{ border: 0, borderRadius: 0, overflow: "visible" }}><div><dt>Tanggal</dt><dd>{dateTime(selected.transactionAt)}</dd></div><div><dt>Kategori</dt><dd>{selected.categoryName || "Belum dikategorikan"}</dd></div><div><dt>Akun</dt><dd>{selected.accountName || "—"}</dd></div><div><dt>Sumber pencatatan</dt><dd>{sourceLabel(selected.sourceType)}</dd></div><div><dt>Status</dt><dd>{statusLabel[selected.status] || selected.status}</dd></div>{selected.type === "TRANSFER" && <><div><dt>Tujuan transfer</dt><dd>{purposeLabel(selected.purpose)}</dd></div><div><dt>Akun kekayaan</dt><dd>{selected.relatedWealthAccountName || "—"}</dd></div></>}</dl>{actionNotice && <div className={`notice${actionNotice.kind === "success" ? "" : " error"}`} role={actionNotice.kind === "success" ? "status" : "alert"}><span>{actionNotice.message}</span>{(actionNotice.kind === "review" || actionNotice.inbox) && <a className="button secondary" href="/inbox?view=transactions">Buka Tinjauan</a>}</div>}{(canEditTransaction(selected) || canVoidTransaction(selected) || canMarkSalary(selected)) && <div className="drawer-actions">{canMarkSalary(selected) && <button type="button" className="secondary" disabled={Boolean(busyAction)} aria-busy={busyAction === "salary" || undefined} onClick={markSalary}>{busyAction === "salary" ? "Menandai…" : "Tandai sebagai gaji"}</button>}{canEditTransaction(selected) && <button type="button" className="secondary" disabled={!editReady} aria-busy={!editReady || undefined} title={!editReady ? "Memuat kategori dan akun…" : undefined} onClick={openEdit}>Ubah</button>}{canVoidTransaction(selected) && <button type="button" className="danger" disabled={Boolean(busyAction)} aria-busy={busyAction === "void" || undefined} onClick={voidTransaction}>{busyAction === "void" ? "Membatalkan…" : "Batalkan transaksi"}</button>}</div>}{selected.activeMerges?.length > 0 && <section className="merge-links" aria-labelledby="merge-links-title"><h3 id="merge-links-title">Penggabungan</h3>{selected.activeMerges.map(merge => <div key={merge.id} className="merge-link"><p>{mergeSummary(merge, money)}</p><button type="button" className="secondary" disabled={Boolean(busyAction)} aria-busy={busyAction === merge.id || undefined} onClick={() => reverseMerge(merge)}>{busyAction === merge.id ? "Memisahkan…" : "Pisahkan penggabungan"}</button></div>)}</section>}<section className="origin" aria-labelledby="origin-title"><h3 id="origin-title">Asal-usul & bukti</h3><div className="timeline">{timeline.origin.map(item => <article key={item.key}><i aria-hidden="true">{item.kind === "evidence" ? "✓" : "•"}</i><div><b>{item.title}</b><small>{item.detail}</small></div></article>)}{!timeline.origin.length && <p className="empty compact">Belum ada sumber pencatatan terhubung.</p>}</div></section><details className="change-history"><summary>Riwayat perubahan{timeline.changes.length ? ` (${timeline.changes.length})` : ""}</summary><div className="timeline">{timeline.changes.map(item => <article key={item.key}><i aria-hidden="true">•</i><div><b>{item.title}</b><small>{item.detail}</small></div></article>)}{!timeline.changes.length && <p className="empty compact">Belum ada perubahan tercatat.</p>}</div></details></>}</aside></div>}</Presence>
  </AppShell>;
}

function drawerUrl(id) {
  const next = new URLSearchParams(window.location.search);
  if (id) next.set("open", id); else next.delete("open");
  const search = next.toString();
  return `/transactions${search ? `?${search}` : ""}`;
}
function purposeLabel(value) { return ({ INTERNAL_TRANSFER: "Transfer internal", SAVINGS_TRANSFER: "Pindah ke tabungan", INVESTMENT_CONTRIBUTION: "Kontribusi investasi", ASSET_PURCHASE: "Pembelian aset", DEBT_PRINCIPAL_PAYMENT: "Pembayaran pokok utang" }[value] || value || "—"); }
function sourceLabel(value) { return ({ BANK_EMAIL: "Email bank", TELEGRAM_TEXT: "Pesan Telegram", TELEGRAM_IMAGE: "Gambar Telegram", WEB_MANUAL: "Input web", WEB_IMAGE: "Dokumen web" }[value] || (value ? evidenceTypeText(value) : "—")); }
