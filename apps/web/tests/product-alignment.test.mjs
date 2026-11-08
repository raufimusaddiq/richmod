import assert from "node:assert/strict";
import { readFileSync, statSync } from "node:fs";
import test from "node:test";
import { reviewCards, tree, globalCss } from "./source.mjs";

const text = path => readFileSync(new URL(`../${path}`, import.meta.url), "utf8");

test("all product routes exist", () => {
  for (const route of ["page.js", "wealth/page.js", "transactions/page.js", "analytics/page.js", "inbox/page.js", "reviews/page.js", "actions/page.js", "documents/page.js", "household/page.js", "settings/page.js"]) {
    assert.ok(statSync(new URL(`../app/${route}`, import.meta.url)).isFile(), route);
  }
});

test("one inbox exposes separate transaction and integration action views", () => {
  const inbox = text("app/inbox/page.js");
  assert.match(inbox, /\/api\/v1\/reviews/);
  assert.match(inbox, /\/api\/v1\/integration-actions/);
  assert.match(inbox, /noopener noreferrer/);
  assert.match(inbox, /user\?\.household\?\.role === "OWNER"/);
  assert.match(text("app/reviews/page.js"), /redirect\("\/inbox\?view=transactions"\)/);
  assert.match(text("app/actions/page.js"), /redirect\("\/inbox\?view=actions"\)/);
});

test("overview chart is backed by deterministic analytics API", () => {
  assert.match(text("app/page.js"), /analytics\/cycle\/daily/);
});

test("charts answer distinct dashboard, cycle, calendar, and category questions", () => {
  const charts = text("app/components/Charts.js");
  const home = text("app/page.js");
  const analytics = tree("app/analytics");
  for (const name of ["DashboardDailySpendingChart", "CycleSpendingPatternChart", "MonthlyCashflowChart", "CategoryDonutChart", "CategoryRankingChart"]) assert.match(charts, new RegExp(`export function ${name}`));
  assert.match(home, /DashboardDailySpendingChart/);
  assert.match(home, /CategoryDonutChart/);
  assert.match(analytics, /CycleSpendingPatternChart/);
  assert.match(analytics, /MonthlyCashflowChart/);
  assert.match(analytics, /CategoryRankingChart/);
});

test("analytics commentary is selected-cycle prose, safely rendered after deterministic evidence", () => {
  const analytics = tree("app/analytics");
  const card = text("app/components/InsightCard.js");
  assert.match(analytics, /api\/v1\/insights/);
  assert.match(analytics, /pollInsight/);
  assert.doesNotMatch(card, /dangerouslySetInnerHTML/);
  const ordered = ["<CyclePosition", 'id="spending-shape"', 'id="changes"', 'id="drivers"', 'id="destinations"', 'id="household"', "<SavingsWealth", "<QualitySection", 'id="discussion"', "<InsightCard"];
  for (let index = 1; index < ordered.length; index += 1) assert.ok(analytics.indexOf(ordered[index - 1]) < analytics.indexOf(ordered[index]), ordered[index]);
});

test("analytics controls are labelled and charts use theme colour tokens", () => {
  const analytics = tree("app/analytics");
  const charts = text("app/components/Charts.js");
  assert.match(analytics, /aria-labelledby="changes-title"/);
  assert.match(analytics, /aria-controls="category-drivers"/);
  assert.match(charts, /var\(--chart-income\)/);
  assert.match(charts, /var\(--chart-expense\)/);
  assert.doesNotMatch(charts, /#[0-9a-f]{3,8}/i);
});

test("transaction filters are query-backed", () => {
  const source = text("app/transactions/page.js");
  for (const name of ["from", "to", "type", "categoryId", "memberId", "status", "accountId", "source", "q"]) assert.match(source, new RegExp(`name=\\"${name}\\"`));
  assert.match(source, /URLSearchParams/);
  assert.match(source, /window\.history\.pushState/);
  assert.match(source, /addEventListener\("popstate", onPopState\)/);
});

test("ledger request failures are recoverable instead of leaving the page loading", () => {
  const source = text("app/transactions/page.js");
  assert.match(source, /<ErrorNotice message=\{error\} retry=\{\(\) => load\(\)\}\/>/);
  assert.match(text("app/components/AuthProvider.js"), /setLoaded\(true\)/);
});

test("manual transactions use an accessible dialog and refresh the ledger", () => {
  const source = text("app/transactions/page.js");
  assert.match(source, /<dialog/);
  assert.match(source, /aria-labelledby="create-transaction-title"/);
  assert.match(source, /fetch\("\/api\/v1\/transactions", \{ method: "POST"/);
  assert.match(source, /await load\(\)/);
});

test("authenticated app shells are not cached across deployments", () => {
  // The shell is a static export embedded in the Go binary; the handler owns the
  // cache classes that next.config headers used to set.
  const handler = readFileSync(new URL("../../api/internal/webui/webui.go", import.meta.url), "utf8");
  assert.match(handler, /no-store, max-age=0/);
  assert.match(handler, /"\/transactions": \{\}/);
  assert.match(text("next.config.mjs"), /output: "export"/);
});

test("mobile shell keeps navigation usable and dismissable", () => {
  const shell = text("app/components/AppShell.js");
  assert.match(shell, /aria-modal="true"/);
  assert.match(shell, /event\.key === "Escape"/);
  assert.match(shell, /aria-controls="mobile-more-panel"/);
  assert.match(shell, /<Icon aria-hidden="true"/);
});

test("web and Telegram share the same review object endpoint", () => {
  assert.match(text("app/inbox/page.js"), /\/api\/v1\/reviews/);
  assert.match(reviewCards(), /classify-transfer/);
  assert.match(reviewCards(), /transactions\?id=/);
  assert.match(reviewCards(), /name="merchantName" required/);
});

test("household route exposes Telegram connection state", () => {
  const source = text("app/household/page.js");
  assert.match(source, /telegramConnected/);
  assert.match(source, /telegram-invite/);
  // An owner created without a Telegram ID links from their own row.
  assert.match(source, /member\.role === "OWNER" && member\.active && !member\.telegramConnected/);
});

test("shared UX feedback is accessible and motion respects user preference", () => {
  const feedback = text("app/components/Feedback.js");
  const styles = globalCss();
  assert.match(feedback, /aria-busy="true"/);
  assert.match(feedback, /role="alert"/);
  assert.match(feedback, /aria-live="polite"/);
  assert.match(styles, /prefers-reduced-motion: reduce/);
  assert.match(styles, /:focus-visible/);
});

test("public routes keep the authenticated overview and a dedicated login flow", () => {
  const home = text("app/page.js");
  const login = text("app/login/page.js");
  assert.match(home, /if \(user === false\) return <LandingPage/);
  assert.match(home, /return <AppShell user=\{user\}/);
  assert.match(login, /useAuth\(false\)/);
  assert.match(login, /\/api\/v1\/auth\/login/);
  assert.match(login, /alt="" aria-hidden="true"/);
});

test("admin console keeps platform tabs and redacts sensitive payloads", () => {
  const admin = tree("app/admin");
  for (const label of ["overview", "jobs", "llm", "logs", "households", "users", "audit"]) assert.match(admin, new RegExp(`"${label}"`));
  assert.match(admin, /\/api\/v1\/admin\/audit\/all/);
  assert.doesNotMatch(admin, /payload_json|last_error|prompt text|raw model output/);
});

test("admin lists use bounded server filters and accessible detail actions", () => {
  const admin = tree("app/admin");
  assert.match(admin, /nextCursor/);
  assert.match(admin, /aria-label="Status tugas"/);
  assert.match(admin, /aria-label="Reference ID"/);
});

test("admin tables keep their column labels when stacked on phones", () => {
  assert.match(tree("app/admin"), /"data-label": headers\[index\]/);
  assert.match(globalCss(), /\.admin-table td::before \{[^}]*content: attr\(data-label\);/);
});

test("admin user changes require confirmation", () => {
  assert.match(tree("app/admin"), /confirm\(/);
});

test("settings lists are bounded with pagination", () => {
  const settings = text("app/settings/page.js");
  for (const dataset of ["accounts", "wealthAccounts", "salarySources", "known", "listeners", "financialSources", "categories", "aliases"]) {
    assert.match(settings, new RegExp(`<BoundedList items=\\{data\\.${dataset}\\}`), dataset);
  }
  assert.doesNotMatch(settings, /settings-list">\{data\.[A-Za-z]+\.map/);
});

test("settings sections are labelled landmarks", () => {
  const settings = text("app/settings/page.js");
  assert.match(settings, /aria-labelledby=\{`\$\{id\}-title`\}/);
  assert.match(settings, /<h2 id=\{`\$\{id\}-title`\}>\{title\}<\/h2>/);
  assert.match(settings, /aria-label="Bagian pengaturan"/);
});

test("forms are reset through a captured element, never event.currentTarget after an await", () => {
  // React clears event.currentTarget once the handler yields, so a reset after
  // await throws and skips the refresh that follows it.
  for (const route of ["household/page.js", "settings/page.js", "documents/page.js", "transactions/page.js"]) {
    assert.doesNotMatch(text(`app/${route}`), /event\.currentTarget\.reset\(\)/, route);
  }
});

test("household invites are copyable, cancellable and show expiry", () => {
  const source = text("app/household/page.js");
  // The link is visible and copyable, not only openable.
  assert.match(source, /Salin tautan/);
  assert.match(source, /navigator\.clipboard\.writeText\(invite\.link\)/);
  // Cancel goes to the endpoint that matches the invite kind (it used to always hit the Telegram one).
  assert.match(source, /INVITE_PATH\s*=\s*\{\s*telegram: "telegram-invite", dashboard: "dashboard-invite"/);
  assert.match(source, /revokeInvite\(invite\.kind, invite\.memberId\)/);
  // Members who already set a password get a reset action, not a first-login invite.
  assert.match(source, /resetting \? "Atur ulang kata sandi" : "Undang masuk ke Richmod"/);
  // Owners can see whether an invite is still valid, expired, or absent.
  assert.match(source, /sudah kedaluwarsa/);
  assert.match(source, /telegramInviteExpiresAt/);
  assert.match(source, /dashboardInviteExpiresAt/);
});

test("forgotten passwords are reset by an invite link, for members and owners", () => {
  const household = text("app/household/page.js");
  assert.match(household, /Atur ulang kata sandi/);
  assert.match(household, /body\.purpose === "RESET"/);
  // An enrolled member gets the reset action instead of the first-login invite.
  assert.doesNotMatch(household, /if \(member\.passwordInitialized\) return null/);
  const admin = text("app/admin/Households.js");
  assert.match(admin, /reset: true/);
  assert.match(admin, /Atur ulang kata sandi pemilik/);
  assert.match(admin, /confirm\(/);
});

// ---- Implicit state machines: every async surface has explicit states ----

const pages = ["inbox", "transactions", "settings", "household", "documents", "wealth"];

test("the shared load helper names its states and guards against stale responses", () => {
  const hook = text("app/lib/useLoadState.js");
  assert.match(hook, /IDLE: "idle", LOADING: "loading", REFRESHING: "refreshing", ERROR: "error", READY: "ready"/);
  assert.match(hook, /new AbortController\(\)/);
  assert.match(hook, /sequence\.current === id/, "a superseded run cannot set state");
  assert.match(hook, /startingStatus\(loaded\.current\)/, "a reload is `refreshing`, not `loading`");
  for (const state of ["closed", "loading", "error", "ready"]) assert.match(hook, new RegExp(`status: "${state}"`), `drawer state ${state}`);
  assert.match(hook, /live\.current\.has\(key\)/, "a per-key busy map refuses a double start");
  assert.match(hook, /finally \{[\s\S]*live\.current\.delete\(key\)/, "busy is released in finally");
});

test("pages with a reload keep rendered children mounted instead of swapping to a skeleton", () => {
  for (const page of ["inbox", "transactions", "wealth", "documents"]) {
    const source = text(`app/${page}/page.js`);
    assert.match(source, /useGuardedLoad\(/, `${page} loads through the guarded hook`);
    assert.doesNotMatch(source, /\{loading \? <Skeleton/, `${page} never swaps children for a skeleton on every reload`);
  }
  assert.match(text("app/inbox/page.js"), /!loader\.hasLoaded && !loader\.failed \? <Skeleton/, "skeleton only before the first load");
});

test("inbox: a 409 refresh keeps the card mounted, busy is per card, history failure is retried", () => {
  const inbox = text("app/inbox/page.js"), cards = text("app/components/ReviewCards.js");
  assert.match(inbox, /response\.status === 409 && result\.missingFacts\) await load\(\)/);
  assert.match(inbox, /useBusyMap\(\)/);
  assert.doesNotMatch(inbox, /setWorking|\[working,/, "no single `working` id");
  assert.match(cards, /busy\[item\.id\]/);
  assert.doesNotMatch(cards, /working === item\.id/);
  assert.doesNotMatch(inbox, /setResolved\(response\?\.ok \? await response\.json\(\) : \[\]\)/, "a failed history fetch is not cached as []");
  assert.match(inbox, /history\.status === "error"/);
  assert.match(inbox, /setHistoryTick\(tick => tick \+ 1\)/, "history can be retried");
  assert.doesNotMatch(inbox, /publishInboxCount[^\n]*\n[^\n]*!error/, "a stale action error does not freeze the badge");
  const resolve = inbox.slice(inbox.indexOf("function resolveAction"));
  assert.match(resolve, /setError\(""\)[\s\S]*try \{[\s\S]*catch/, "resolveAction clears the error and catches");
});

test("inbox count: a failed response keeps the last known count instead of claiming zero", () => {
  const provider = text("app/components/InboxCountProvider.js");
  assert.match(provider, /if \(!responses\.every\(response => response\.ok\)\) return;/);
  assert.doesNotMatch(provider, /response\.ok \? response\.json\(\) : \[\]/);
});

test("auth: only a 401 signs the user out", () => {
  const auth = text("app/components/AuthProvider.js");
  assert.match(auth, /response\.status === 401/);
  assert.doesNotMatch(auth, /catch \{[^}]*setUser\(false\)/, "a network error is not a logout");
  assert.match(auth, /setTimeout\(\(\) => load\(attempt \+ 1\)/, "a blip retries");
  assert.match(text("app/household/page.js"), /meResponse\.status === 401/);
  assert.match(text("app/admin/page.js"), /err\?\.status === 401/);
});

test("admin: drawers reset per record, loads can fail visibly, lists guard load-more", () => {
  const households = text("app/admin/Households.js"), shared = text("app/admin/shared.js");
  assert.match(households, /<HouseholdDetail\s+key=\{selected\}/, "a household drawer remounts per household");
  assert.match(households, /useEffect\(\(\) => \{ setCopied\(""\); \}, \[link\]\)/, "copied resets when the link changes");
  assert.match(households, /ownerInvite\.kind === "invite" && !pendingOwner\) setOwnerInvite\(null\)/, "a spent owner link is cleared");
  assert.match(shared, /export function Pending\(/);
  assert.match(shared, /result\.url === url/, "useLoad never returns another url's data");
  assert.match(shared, /moreFlight\.current/, "load-more has an in-flight guard");
  assert.match(shared, /if \(id !== generation\.current\) return;/, "load-more from an old filter is discarded");
  for (const file of ["Households", "Overview", "Users", "Jobs", "Logs", "Audit", "LLM", "Reviews"]) {
    assert.match(text(`app/admin/${file}.js`), /<Pending state=/, `${file} shows load errors with retry`);
    assert.doesNotMatch(text(`app/admin/${file}.js`), /<Empty>Memuat/, `${file} has no unconditional "Memuat…"`);
  }
  const page = text("app/admin/page.js");
  assert.match(page, /const select = \(key\) => \{ setError\(""\)/);
  assert.match(page, /useEffect\(\(\) => \{ setError\(""\); \}, \[tab\]\)/);
  const users = text("app/admin/Users.js");
  assert.match(users, /ADMIN_SELF_LOCKOUT/);
  assert.match(users, /useBusyMap\(\)/);
  assert.match(users, /disabled=\{Boolean\(busy\[user\.id\]\)\}/);
});

test("transactions: retry reloads with current filters, the drawer is aborted state, failure drops rows", () => {
  const source = text("app/transactions/page.js");
  assert.doesNotMatch(source, /retry=\{load\}/, "a click event is never passed as `search`");
  assert.match(source, /useDrawerRecord\(/);
  assert.match(source, /\{ signal \}/, "detail requests are abortable");
  assert.doesNotMatch(source, /setDetailLoading|setDetailError/);
  assert.match(source, /onFailure: \(\) => \{ setItems\(\[\]\)/, "no stale rows under the banner");
});

test("settings: request is try/catch/finally with per-key busy, load failures surface, toggles lock while busy", () => {
  const source = text("app/settings/page.js");
  const view = source.slice(0, source.indexOf("// The state model."));
  const request = view.slice(view.indexOf("function request("), view.indexOf("async function createAccount"));
  assert.match(request, /setError\(""\)[\s\S]*try \{[\s\S]*catch/);
  assert.match(view, /useBusyMap\(\)/);
  assert.doesNotMatch(view, /setWorking|working ===/);
  assert.doesNotMatch(view, /response\.ok \? response\.json\(\) : null/, "a failed endpoint is not silently null");
  assert.match(view, /if \(!loader\.hasLoaded\) return/, "no empty-looking settings before a successful load");
  const toggles = [...view.matchAll(/onClick=\{\(\) => patch\(item\.id,/g)];
  const locked = [...view.matchAll(/disabled=\{isBusy\(item\.id\)\} onClick=\{\(\) => patch\(item\.id,/g)];
  assert.ok(toggles.length >= 6 && locked.length === toggles.length, "every toggle is disabled while busy");
  const test_ = view.slice(view.indexOf("async function testFinancialSource"), view.indexOf("const emailIngressAction"));
  assert.match(test_, /try \{[\s\S]*catch/);
  assert.match(test_, /30 detik/, "the polling timeout is reported");
  assert.match(view, /<form key=\{editingFinancialSource\.id\}/, "the provider edit form remounts per provider");
});

test("household: busy map, cleared errors, member load failures, invite expiry ticks", () => {
  const source = text("app/household/page.js");
  assert.match(source, /useBusyMap\(\)/);
  assert.match(source, /membersState/, "an unloaded member list is not '0 akun'");
  assert.match(source, /setInterval\(\(\) => setNow\(Date\.now\(\)\), TICK_MS\)/);
  assert.doesNotMatch(source, /new Date\(iso\)\.getTime\(\) > Date\.now\(\)/, "validity is not computed at render time");
  const deactivate = source.slice(source.indexOf("async function deactivate"), source.indexOf("if (!me)"));
  assert.match(deactivate, /setError\(""\)/);
  assert.match(deactivate, /current\?\.memberId === memberId \? null : current/, "deactivating closes that member's invite panel");
  const revoke = source.slice(source.indexOf("async function cancelInvite"), source.indexOf("const revokeInvite"));
  assert.match(revoke, /setError\(""\)[\s\S]*try \{[\s\S]*catch/);
});

test("documents: the drawer separates loading, error and ready, and upload is guarded", () => {
  const source = text("app/documents/page.js");
  assert.match(source, /useDrawerRecord\(/);
  assert.match(source, /drawer\.status === "ready" && !extractions\.length/, "'still processing' only for a ready drawer");
  assert.match(source, /onClick=\{drawer\.retry\}/);
  const upload = source.slice(source.indexOf("async function upload"), source.indexOf("function open("));
  assert.match(upload, /try \{[\s\S]*catch[\s\S]*finally \{ setWorking\(false\)/);
  assert.match(source, /setInterval\(\(\) => load\(\), 5000\)/, "status is refreshed after an upload");
});

test("invite: busy flag, network error message, error cleared on edit", () => {
  const source = text("app/invite/page.js");
  assert.match(source, /if \(busy\) return;/);
  assert.match(source, /try \{[\s\S]*catch \{[\s\S]*Koneksi terputus/);
  assert.match(source, /setError\(""\)/);
  assert.match(source, /disabled=\{busy\}/);
});

test("wealth: drawer states, guarded saves, and a failed reload drops the old figures", () => {
  const source = text("app/wealth/page.js");
  assert.match(source, /useDrawerRecord\(/);
  assert.match(source, /onFailure: \(\) => setData\(empty\)/);
  for (const name of ["saveSnapshot", "correctSnapshot"]) {
    const body = source.slice(source.indexOf(`async function ${name}`));
    assert.match(body.slice(0, body.indexOf("\n  }\n")), /try \{[\s\S]*catch[\s\S]*finally \{ setWorking\(false\)/, `${name} releases busy in finally`);
  }
});

test("no state-machine page keeps the old single-string `working` or a bare loading boolean", () => {
  for (const page of pages) assert.doesNotMatch(text(`app/${page}/page.js`).split("// The state model.")[0], /useState\(true\), \[error|const \[loading, setLoading\]/, page);
});
