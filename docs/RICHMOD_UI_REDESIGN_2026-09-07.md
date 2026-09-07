# Richmod UI Redesign — September 7, 2026

## Scope

This redesign is a frontend-only product refinement. It preserves existing
routes, API contracts, evidence records, review flows, household scoping, and
the Go-owned financial mutation boundary.

## Audit

The prior web product had a sound information architecture, but its visual
language weakened the product's financial and evidence-first intent:

- global styles were a difficult-to-maintain minified rule set;
- navigation mixed text glyphs and bespoke SVGs;
- overview gave every value equal visual weight;
- ledger rows did not prioritize scan order consistently on small screens;
- review, document, settings, and admin surfaces shared inconsistent spacing,
  borders, feedback states, and responsive treatment;
- mobile behavior existed, but navigation and dense actions still read as a
  compressed desktop interface.

## Design Decisions

- **Quiet financial foundation:** warm neutral canvas, white evidence
  surfaces, a restrained evergreen action color, and semantic income, expense,
  review, and error colors.
- **Numerical hierarchy:** tabular numerals, a single primary cashflow value on
  Overview, supporting income and expense figures, and right-aligned ledger
  amounts.
- **Evidence-first surfaces:** documents, review cards, drawers, and ledger
  states keep source, confidence, confirmation, and required human action
  visibly distinct without inventing data.
- **One icon family:** navigation and financial row icons use Phosphor Icons.
- **Mobile is intentional:** a fixed five-item mobile navigation, a focused
  more-menu, compact ledger rows, stacked review forms, fluid filters, and
  mobile admin table cards prevent normal-use horizontal overflow.
- **Accessible defaults:** visible keyboard focus, a skip link, semantic
  navigation, labelled icon controls, touch-sized actions, reduced-motion
  support, and statuses with text rather than color alone.

## Frontend Conventions

- Use tokens from `apps/web/app/globals.css`; do not introduce route-local
  colors, shadows, radii, or arbitrary spacing without a product need.
- Use `.surface` for meaningful grouped content only. Prefer separators and
  hierarchy to nested cards.
- Use `.section-title`, `.status`, `.empty-state`, `.notice`, and the existing
  table/list patterns before adding a new UI primitive.
- Keep transaction amounts tabular and end-aligned. Preserve evidence and
  review state even when space is constrained.
- Keep semantic colors reserved for a financial meaning or an actionable
  system state.

## Brand Color Separation — September 7, 2026

Richmod uses a restrained dried-plum accent for brand and interaction states.
Evergreen remains reserved for income, confirmation, and positive financial
meaning; terracotta remains expense/outflow; ochre remains review/warning; red
remains danger; slate remains informational. The categorical chart palette is
balanced across plum, slate, copper, olive, clay, and muted violet rather than
repeating the financial-positive green family.

The authenticated shell fills the viewport after the fixed sidebar. Readable
width constraints belong to individual forms or text surfaces, never the global
`.app-main` container.

## Deliberate Limits

No new backend endpoints, financial calculations, persistence behavior, or
infrastructure were added. The design uses the data already supplied by the
existing application; more granular evidence indicators belong in a separate
product/API change only if the backend exposes a reliable field for them.

## Public Experience — September 7, 2026

Logged-out `/` extends the authenticated product identity into a warm,
editorial landing page. Its primary story is evidence → validated understanding
→ household ledger, with Review Inbox shown as the explicit branch for
uncertainty. `/login` is a separate focused surface using the existing
email/password endpoint and session behavior. Public styles use scoped
`public-*`, `landing-*`, and `login-*` selectors; they must not change AppShell
or authenticated route layout.

## Recovery Audit — September 7, 2026

The redesign recovery compared `main` with pre-redesign commit
`f52a91c39fda9a7930c5199f0930e24977ac360b` and inspected the rendered product at
1440×900, 1024×768, and 390×844 using deterministic Playwright fixtures.

### Root Causes

- Global form defaults forced unchanged household/settings controls to full width.
- The member layout assumed fewer fields than the current household markup.
- The desktop ledger minimum columns exceeded the available tablet shell width.
- The overview KPI grid allowed long formatted amounts to collide.
- The mobile shell breakpoint left a sidebar active at narrow desktop widths.
- Transaction drawers and native dialogs lacked safe layout foundations.

### Recovery Conventions

- Global rules provide safe defaults only; route behavior uses explicit classes.
- Tablet shell behavior starts at the observed layout transition (`1100px`).
- Ledger columns use bounded minimums; mobile rows use a separate compact layout.
- Financial values use bounded, tabular typography and preserve scan order.
- Drawers, dialogs, forms, and member actions define their own responsive layout.

### Verification

`apps/web/scripts/visual-smoke.mjs` intercepts API calls with synthetic data only.
It captures every major authenticated route at all three target viewports, checks
normal-use horizontal overflow, checks browser/page errors, exercises transaction
and document drawers, the manual transaction dialog, mobile navigation, and login.
Generated screenshots are ignored under `apps/web/test-results/`.

## Analytics Visual Recovery — September 7, 2026

Analytics now uses semantic `<strong>` KPI values, an explicit page flow, scoped
ranked-card padding, deterministic range-control layouts, and structured chart
tooltips. Chart colors come from Richmod chart tokens derived from income,
expense, reference, axis, grid, and categorical product colors; `.surface`
remains appearance-only. Meaningful metadata uses `--muted`; `--faint` remains
limited to placeholders and genuinely secondary text.

Visual regression baselines live in `apps/web/tests/visual-baselines/` and are
compared by `apps/web/scripts/visual-smoke.mjs` with deterministic synthetic API
fixtures. Baseline updates require `UPDATE_VISUAL_BASELINES=1`.
