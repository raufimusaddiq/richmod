# Richmod brand guidelines

## Concept

**Retro Ledger.** Richmod is a household ledger, so the product borrows the
vocabulary of a well-kept paper book: cream stock, ruled lines, ink outlines,
offset shadows, and a display serif reserved for headings. The surfaces stay
calm and legible because the money is the loud part, not the chrome.

The single token source is `apps/web/app/styles/01-tokens-and-base.css`
(`:root`). `apps/web/app/globals.css` is only an ordered list of `@import`s over
the pieces in `apps/web/app/styles/`; the pieces were cut from the original
stylesheet in order, so the cascade is the import order. Keep that order and
add rules to the piece that owns them (a test checks the structure). This document describes
that source; it does not introduce a parallel token file.

## Palette

### Primary Colors

| Role | Token | Hex | Use |
| --- | --- | --- | --- |
| Canvas | `--canvas` | `#f7f3e8` | Page background |
| Canvas deep | `--canvas-deep` | `#eee8d8` | Recessed bands, footers |
| Surface | `--surface` | `#fffcf4` | Cards, panels |
| Surface strong | `--surface-strong` | `#fffef9` | Inputs, tooltips, menus |
| Surface muted | `--surface-muted` | `#f2edde` | Hover fills, inert rows |
| Ink | `--ink` | `#253a36` | Text, 2px outlines, offset shadows |
| Ink soft | `--ink-soft` | `#354943` | Secondary text and labels |
| Muted | `--muted` | `#4c5a50` | Metadata |
| Faint | `--faint` | `#596359` | Placeholders only |
| Line | `--line` | `#d5d9c8` | Hairline separators |
| Line strong | `--line-strong` | `#7b8979` | Input and control borders |
| Accent | `--accent` | `#245c54` | Primary actions, links, focus |
| Accent hover | `--accent-hover` | `#17483f` | Action hover and pressed |
| Accent soft | `--accent-soft` | `#dceae2` | Accent tinted surface |

### Secondary Colors

| Role | Token | Hex | Use |
| --- | --- | --- | --- |
| Butter | `--butter` | `#f5ebc3` | Retro highlight, review band |
| Coral | `--coral` | `#cb715c` | Warm accent for illustration only |

### Semantic Colors

| Role | Token | Hex | Use |
| --- | --- | --- | --- |
| Income | `--income` | `#315b3f` | Confirmed inflow; lower spending (▼) in a spending-change delta |
| Expense | `--expense` | `#8b4437` | Outflow; higher spending (▲) in a spending-change delta |
| Warning | `--warning` | `#73561e` | Review and incomplete data |
| Danger | `--danger` | `#913e40` | Destructive and failed states |
| Info | `--info` | `#3a5867` | Informational state |

### Rules

- Accent is the only brand colour used for interaction. There is one accent per
  viewport; income, expense, warning, danger, and info are reserved for financial
  meaning or an actionable system state.
- Accent budget per above-the-fold viewport: primary action, one key metric, and
  active navigation.
- Data marks (bars, lines, matrix cells) never use the accent: they use `--ink`,
  `--ink-soft` or the chart tokens. Red (`--expense`, `#8b4437`) and green
  (`--income`, `#315b3f`) are reserved strictly for spending-change direction
  ("spent more / spent less than last cycle": `Yang berubah` bars and deltas,
  the `Keluar` KPI delta, and Beranda's cycle comparison line), always paired
  with ▲/▼ and the neutral wording `lebih rendah` / `lebih tinggi` (never
  `lebih hemat` or `lebih boros`), and appear nowhere else in category or hero
  figures. A selected cycle, row or list item is `--butter`.
- Category colours (`--chart-category-1..8`: slate `#3a5867`, ochre `#73561e`,
  violet `#705268`, light slate `#536e80`, dusk indigo `#544d73`, warm sand
  `#6b5842`, dusty blue `#435c86`, muted plum `#6a4c78`; plus pale sand
  `--chart-category-other` `#cfc9b6` for `Kategori lain`) contain no red, no green,
  and no accent teal, so a category never reads as a spending direction.
  One shared mapping (`categoryColorMap`, keyed by category identity rather
  than list position) drives Beranda "Ke mana uangnya", Laporan "Ke mana
  uangnya", "Siklus ke siklus", per-category tables, the category drawer, and
  calendar category bars, so each category keeps one colour across screens and
  cycles with no repeated colours among the displayed categories; `Kategori
  lain` always uses `--chart-category-other` (sand). Every
  `--chart-category-1..8` token clears WCAG AA 4.5:1 against `--surface-strong`.
- Text tokens must keep WCAG AA 4.5:1 against every surface token. This is
  enforced by `apps/web/tests/ui-audit-locks.test.mjs`; run it before shipping a
  palette change.

## Typography

| Role | Family | Weight | Notes |
| --- | --- | --- | --- |
| Display | Fraunces (variable, self-hosted; weight and `SOFT` axes) | 500-600 | `h1`, `h2`, `h3`, brand wordmark |
| Body and UI | Inter (variable, self-hosted) | 400-600 | Paragraphs, controls, tables, chart ticks |
| Numerals | Inter | 400-600 | `font-variant-numeric: tabular-nums` on every amount |

Both files are Latin subsets under `apps/web/public/fonts/`, loaded with
`display: swap` and a local sans-serif fallback. Keep the text floor at 12px
(`--text-xs`); `ui-audit-locks.test.mjs` fails on smaller text. Small links and
tabs keep a 32px hit area (44px on touch devices).

Headings use Fraunces at `SOFT` 100 (`--font-display-soft`), which rounds the
serifs and thickens hairlines so card titles do not read as a default Times
face. The bundled file carries the weight and `SOFT` axes only; it has no
optical-size axis.

### Font Stack

Heading family: "Fraunces" (`--font-display` in CSS).
Body family: "Inter" (`--font-body` in CSS).

## Shape and depth

- Radius steps are tokens, not one repeated value: `--radius-xs` 8px,
  `--radius-sm` 12px, `--radius-md` 18px, `--radius-lg` 26px.
- Depth comes from a 2px ink outline plus an offset shadow (`--shadow`,
  `--shadow-float`). No blur-based glow.
- Controls keep a 44px minimum height and every visible input, select, and
  textarea stays at 16px or larger on mobile to prevent iOS focus zoom.
- Keyboard-aware mobile forms: a client hook (`KeyboardViewport`) observes
  `window.visualViewport` (`resize` and `scroll`) to set `--kb-inset` and
  `--vv-height` on `html`, hides `.mobile-nav` while an editable field is
  focused or the keyboard is open, and scrolls the focused field to center on
  focus and viewport resize. On phones (`<= 719px`), bottom sheets and dialogs
  (Catat, Ubah, `useDialogs` prompts) anchor to `bottom: var(--kb-inset)` with
  `max-height: min(92dvh, calc(var(--vv-height) - 8px))`, drawers shrink to
  `var(--vv-height)`, and focused inline form submit bars (Wealth "Perbarui
  nilai", Keluarga forms, CycleDecisions) stick above `var(--kb-inset)`.
- Mobile keyboard hints: Catat amount uses `inputMode="numeric"` and
  `enterKeyHint="next"` (Enter advances to description rather than submitting
  half-filled), description uses `enterKeyHint="done"` and
  `autoCapitalize="sentences"`, search fields use `enterKeyHint="search"`,
  login uses `next`/`go`, merchant/place and member names use
  `autoCapitalize="words"` (`autoCorrect="off"` on merchant/institution names),
  note textareas use `autoCapitalize="sentences"`, wealth decimals use
  `inputMode="decimal"`, and Keluarga account hints use `inputMode="numeric"`
  with `pattern="[0-9]*"`.
- Native date/date-time inputs and label tracks must shrink inside their
  container. Transaction dialogs use one padded surface, not nested panels;
  date filters and manual entry stay usable at 320px without horizontal scroll.

## Voice and vocabulary

Web and Telegram speak Indonesian with one name per concept. Internal domain
terms (Wealth Account, Review Inbox, household, residual) stay in code, schema,
and PRDs; they never appear in user-facing copy.

| Concept | User-facing term | Not |
| --- | --- | --- |
| Wealth Account | akun kekayaan | Wealth Account, Akun Wealth |
| Review Inbox | Kotak Tinjauan | Review Inbox |
| review (noun) | tinjauan | review |
| household | keluarga | household |
| salary cycle / residual | siklus gaji / sisa siklus gaji | salary cycle, residual |

- Labels and card badges use sentence case, not ALL CAPS.
- Telegram sounds like a helpful conversation: natural `aku`/`kamu`, short
  sentences, a clear next step. Generated replies match the user's language.
- Use emojis sparingly (0–2 in conversational replies); retain words so status
  never depends on an icon. Reserve success markers such as ✅ for completed
  actions. Pending confirmation, ambiguity, and errors must not imply recording
  succeeded. Avoid celebratory spending language or financial judgment.
- Telegram dates use Indonesian month names (`04 Mei 2026`, `01 Agu 2026`)
  through `formatIDDate*` in `apps/worker/internal/telegram/id_format.go`; never
  Go's `time.Format` with a month name.
- Telegram asks for confirmation with inline buttons (`pending:action:*`,
  `pending:batch:*`), not by asking the user to type yes/no. Typed answers still
  work through the bounded judgment lane.
- Telegram error messages say what to type next ("Sebutkan harinya, misalnya
  hari ini atau 12 Agu"), never "tidak valid" on its own.
- A confirmation that is answered, or a button that is no longer valid, is
  retired: the original message is edited to keep its text, drop its buttons,
  and append the outcome ("Dikonfirmasi.", "Dibatalkan.", "Tidak lagi
  berlaku."), so a second tap cannot reach a resolved question.
- Telegram has one help text (`helpMessage`). Typed `/help` and `/start` answer
  with it deterministically, before any model call. The agent prompt tells the
  model to say what Richmod can do and point at `/help` whenever it declines a
  request. The worker registers a `/help` command menu at start (best-effort).
- Confirmations and text entry use `useDialogs` (a native `<dialog>`), never
  `window.confirm`/`window.prompt`. The one exception is the unsaved-draft guard
  in analytics, which must answer synchronously.
- The app shell reads one shared count (`InboxCountProvider`): loaded once per
  session, refreshed on tab focus (at most once a minute), and set exactly by the
  inbox page whenever it holds both lists. It exposes `reviewCount` (money
  decisions, "Perlu dicek") and `actionCount` (owner integration setup actions)
  apart; only `reviewCount` drives the navigation, `actionCount` marks
  Keluarga › Sumber otomatis.
- Modal drawers (`aside[role="dialog"]`) use `useDrawerA11y`: focus moves in,
  Escape closes, Tab stays inside, and focus returns to the opener. While any modal
  dialog or drawer is open, background scroll is locked on `html` with
  `overscroll-behavior: contain` inside the drawer, and on phones drawers render as
  a single full-screen scroll container.
- Pages, sheets, and cards avoid sideways scrolling across 320–1366 px viewports (no
  `100vw` widths); wide detail tables (`.review-table-wrap`, `.merchant-rank-scroll`)
  stack into label/value rows at `<= 600px`.
- Navigation has four permanent places: **Beranda** (`/`), **Transaksi**
  (`/transactions`), **Laporan** (`/analytics`; `/wealth` highlights it) and
  **Keluarga** (`/household`; `/settings` renders the same hub and highlights it).
  There is no overflow menu. `/documents` stays reachable from the + Catat sheet
  ("Lihat semua riwayat foto bukti ›"), not from the navigation.
- **Perlu dicek** (`/inbox`) is conditional: a butter-highlighted sidebar link with
  the count, and a compact "N perlu dicek ›" butter pill in the phone page header,
  both rendered only while `reviewCount > 0`. The direct URL always works.
- **Perlu dicek cards** ask one plain Indonesian question with ready 1-tap answers
  (`Masuk kategori apa?` shows up to three history-ranked categories from
  `suggestedCategories`, `sering dipakai` only on a merchant-history first rank, then
  `Kategori lain…`, plus an `Ingat untuk <tempat>` checkbox). Cards use the calm
  supporting-card style (1.5px `--line-strong`, no offset shadow). A resolved card
  collapses into one line `✓ <tempat> · <jumlah> <hasil> · Urungkan` (reopen). The
  page has two tabs, `Menunggu (N)` and `Riwayat 30 hari`; `Tindakan` and `Email masuk`
  live in Keluarga › Sumber otomatis and the old `?view=actions|email` links move there.
- Desktop sidebar order: brand, **+ Catat**, conditional Perlu dicek, the four places,
  then workspace, a quiet **Admin** link (super admins only) and the user.
- Phone bottom bar: `Beranda · Transaksi · + · Laporan · Keluarga`. The centre **+**
  is the one elevated control (52px, 2px ink border and offset shadow); every tab is
  at least 44px and the bar clears `env(safe-area-inset-bottom)`.
- **+ Catat** is one global sheet (modal on desktop, bottom sheet on a phone) that
  opens straight onto the manual form: Pengeluaran | Pemasukan | Transfer, an empty
  live thousands-grouped Rp amount input (example placeholder, never prefilled `0`,
  autofocused on open), "Ulangi cepat" in one horizontally scrollable chip row
  directly under amount so it stays visible above the keyboard,
  a description with a deterministic category suggestion, the six most-used categories
  as 1-tap chips (+ "Lainnya…"), with "Ulangi cepat" chips sourced from
  `GET /api/v1/transactions/quick-suggestions` (Go only, no AI; sourced from the
  household's own `WEB_MANUAL` and `TELEGRAM_TEXT` entries, deduplicated by
  type/description/amount, ranked most frequent then most recent, with short labels).
  **Foto bukti** sits beside **Catat** and swaps the body for the multi-image
  upload with live status and "Proses ulang". Inputs are 16px, the save bar is
  sticky above the keyboard, and the sheet keeps the 2px ink border and offset
  shadow. Saving closes it, shows "Tercatat · Urungkan" (void) and fires
  `richmod:transaction-created` so open pages refresh.
- **Keluarga** is one workspace with five deep-linkable tabs
  (`?tab=anggota|akun|kategori|sumber|notifikasi`): Anggota (member list above
  the add-member form), Akun & kekayaan,
  Kategori & aturan, Sumber otomatis (with the owner's pending "Tindakan" and the
  "Email masuk terakhir" log) and Notifikasi. A member sees Anggota (read-only)
  and Notifikasi only. Below the desktop sidebar breakpoint (`<= 1100px`), super
  admins see an **Admin Richmod** entry row at the top of Keluarga linking to
  `/admin`. Every old `/settings?tab=…` and `/settings#<anchor>` link opens the
  matching tab and scrolls to the anchor.
- **Beranda** keeps one 2px ink-outlined, shadowed card: the hero. Supporting cards
  (`.home-card`: Pengeluaran harian, Ke mana uangnya, Terbaru, Kekayaan bersih) use a
  1.5px `--line-strong` border on `--surface` with no shadow. The hero has three
  states: empty (welcome + "Catat transaksi pertama" / "Foto bukti"), expenses-only
  ("Pengeluaran siklus ini" + "Catat pemasukan", no misleading negative balance) and
  normal ("Sisa siklus ini" with spending share, the comparison against the previous
  cycle/month from `GET /api/v1/analytics/overview`, and four figures). Hero
  Sisa/Keluar and the four summary figures render in `--ink`, not green or red;
  only the cycle comparison delta uses `--income` / `--expense`. On phones
  (`<= 680px`), Beranda hides the daily chart and Kekayaan card because both live
  in Laporan; on desktop, the Kekayaan card aligns to its content height with no
  empty space underneath. All cards read
  the current period (`categories?period=current_cycle`); category rows and non-zero
  day bars open `/transactions` filtered to that category/day. Daily bars use
  neutral ink (`--ink-soft`), reserving red for "spent more than last cycle", and
  cap near the 90th-percentile day (snapped to a round axis top when the peak
  exceeds twice that cap) so at most ~3 spikes are clipped with a break slash; the
  y-axis uses round ticks labelled in `rb` below 1 jt (`0`, `250rb`, `500rb`) and
  clipped bar labels stagger on nearby spikes (or show only the largest on narrow
  widths). Owners see a one-line butter notice when `actionCount > 0`.
- **Laporan** leads with a 4-tile ink-outlined KPI strip (no Refund/explainer
  footer line). "Ke mana uangnya" shows category colour, amount, and share (%)
  only (changes belong to "Yang berubah"). "Siklus ke siklus" draws stacked bars
  split by category using the same category colours as "Ke mana uangnya", and
  `Angka per kategori ›` opens the exact per-category table. Below the charts,
  one calm card holds uniform collapsed disclosure rows (`14px 16px` padding,
  17px Fraunces title in `--ink`, 13px muted summary on its own line below, `+`
  aligned with the title): `Pengeluaran harian` and `Obrolan keluarga` (grouping
  Catatan rumah tangga, Bahan pembahasan, and Keputusan rumah tangga as sections
  inside), plus `Kelengkapan data` when there are no blockers.
- **Transaksi** is a quiet ledger grouped by Asia/Jakarta day with each day's net spend
  in the header. Rows show a display name preferring merchant name → counterparty
  name → description (and `Transfer ke <name>` for transfers with a counterparty,
  display-only), a semantic Phosphor category icon (`categoryIcons.js`, eight
  category tones, never letter boxes), one meta line and the signed amount; a badge
  appears only for exceptions (Perlu dicek, Dibatalkan). Voided rows are hidden through
  `exclude_voided=true` (response headers `X-Total-Count` and `X-Voided-Count`) unless
  a status filter or `?showVoided=1` is set. Filters are a search box and chips
  (Periode, Kategori, Anggota, Akun) plus a "Filter lain" panel (Tipe, Status, Sumber,
  Dari/Sampai tanggal). The drawer shows "Asal-usul & bukti" and a collapsed
  "Riwayat perubahan".

## Diagrams and charts

- Diagrams: 2px ink outlines, pastel fills, ink arrows, and `Inter` labels with
  `Fraunces` reserved for headline text.
- Charts: 2px ink outlines on bars and slices, a sparse horizontal grid, axis
  labels at 11px, direct labels over legends where possible, no 3-D or truncation.
- Insight copy is supporting text. Priority figures (cycle position, comparisons,
  evidence links, data-quality warnings) stay visible; explanatory prose sits
  behind a `Tentang data ini` disclosure so the data reads before the narrative.
- Learned constraint: complete reporting must not require reading a novel.
  Lead with figures and labelled comparisons; use explicit expandable evidence
  and a complete-report action. Do not merely collapse explanatory paragraphs
  while leaving repeated totals and tables at equal visual weight.

## Assets

### Logo brandkit

The logo extends **Retro Ledger** with three rounded page fields separated by
a vertical and diagonal negative-space seam. This is the vector recreation of
the selected geometric-ledger concept (option 3). The mark uses cream
`#f7f3e8` on teal `#245c54` in avatars; the transparent symbol uses teal fields.
The wordmark uses ink `#253a36`. Butter remains a UI highlight, not a logo detail.
The serif wordmark is supplied as outlined paths in SVG; it does not depend on
an installed font and does not change the UI's Fraunces/Inter font stacks.

The full logo uses a smaller circular mark beside the wordmark, with the
tagline directly below the name and no separator rule. The name is the primary
element; the mark and tagline support it.

The canonical distributable files are under `docs/assets/brand/`:

| Asset | Files | Use |
| --- | --- | --- |
| Full logo | `richmod-full-logo.svg`, `.png` (5760 × 1920) | README header, cream backgrounds; includes “Keuangan keluarga, tanpa menebak.” |
| Horizontal logo | `richmod-horizontal-transparent.svg`, `.png` (4500 × 1500) | Mark and wordmark on a light surface |
| Symbol | `richmod-symbol-transparent.svg`, `.png` (4096 × 4096) | Standalone mark without a wordmark |
| Telegram avatar | `richmod-telegram-avatar.svg`, `.png` (4096 × 4096), `richmod-telegram-avatar-1024.png` | Circular profile; upload the 1024 × 1024 PNG |

- The Telegram artwork is circular inside a square transparent canvas. Keep
  the three page fields centered inside the circle with room for Telegram's
  circular crop; do not add a rounded-square tile, literal initial, or text.
  Use the 1024 × 1024 PNG for the bot profile.
- SVG wordmarks are outlined, so they render the same without installed fonts.
- Keep each asset's aspect ratio. Scale from SVG when another size is needed;
  never enlarge a small raster export as the source for a larger one.
- Preserve the three fields and their negative-space seam. Do not add ruled
  lines, folded corners, gradients, glows, or financial status colors to the mark.
- Transparent wordmarks use dark ink and need a light background. The full
  logo already includes its cream background.

### Product assets

| Asset | Use |
| --- | --- |
| `apps/web/app/icon.svg` | Favicon; circular teal field and cream ledger-page mark |
| `docs/assets/richmod-logo.svg` | Legacy documentation path; same circular ledger-page mark |
| `apps/web/public/brand/richmod-evidence-flow.svg` | Landing evidence flow |
| `apps/web/public/brand/richmod-login-treeline.svg` | Login footer illustration |
| `apps/web/app/opengraph-image.js` | Social card |
