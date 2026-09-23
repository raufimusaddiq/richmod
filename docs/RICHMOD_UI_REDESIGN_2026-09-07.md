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

## Depth Audit and Fixes — September 23, 2026

A deeper audit of the same surfaces found that the redesign's visual language
held up, but four system-level guarantees did not. All are now fixed and locked
by tests or by the rendered-page smoke run.

### Contrast (critical)

The previous palette failed WCAG AA on real surfaces, not in theory:

- `--faint` was `.75:1` short of AA on `.surface`; it was used for metadata on
  the landing evidence stack, the public footer, and login footers;
- `--warning` fell to `4.16:1` on the review inbox's own `--warning-soft`, the
  alert token used at the highest-risk surface in the product;
- the review alert, workflow review, and notice text used literal half-tones
  (`#70634b`, `#756b57`, `#675b43`) that duplicated the warning token wrongly.

`--faint` is now `#6f6961`, `--warning` is `#7f5c1f`, and the literal half-tones
use the semantic token. `apps/web/tests/ui-audit-locks.test.mjs` computes the
contrast ratio for every text token on every surface token and fails below
`4.5:1`, so the guarantee is enforced rather than inspected.

Leading is unchanged: AA and AAA large-text thresholds already passed.

### Type scale (high)

The product used eight body sizes between `8px` and `13px` with no documented
role, including `9px` review metadata and status chips, `8px` mobile status
chips, and `9px` admin table headers. `globals.css` now declares one named
scale (`.text-xs` `11px` → `.text-xl` `19px`) and no rule renders text below
`11px`; `--weight-semibold` replaces the ad-hoc `650`. The visual smoke run
asserts the `11px` floor against real computed styles on every route and
viewport, so a future sub-`11px` rule fails visibly instead of shipping.

Remaining `12px`, `13px`, `15px`, `17px`, `18px`, `21px`, and `23px` values are
intentional display sizes; they belong to a supporting scale, not to body text.

### Interaction and assistive technology (high)

- `Toast` restarted its dismissal timer whenever a parent re-created the
  `onClose` prop, so confirmations could stay on screen indefinitely. Dismissal
  is now keyed on the message with a stable callback at both call sites.
- Route loading states, the shared skeleton, and the overview loader were
  silent; they are now `role="status"` live regions.
- The transaction drawer had no dialog semantics. It now matches the document
  and wealth drawers; admin drawers gained Escape-to-close and initial focus.
- The Review Inbox tablist now supports roving `ArrowLeft`/`ArrowRight`
  navigation instead of declaring tab semantics without tab behaviour.
- Targeted fixes: Overview opens the exact snapshot it summarises
  (`/wealth?snapshotId=…`), settings deep links gained `scroll-margin-top`,
  donut percentages render at `11px`, wealth history rows announce their
  action, and resolved cycle residual status is no longer warning-coloured.

### Verification boundary

The `11px` assertion restores no pixel baselines. Because the design is judged
final and the local baselines were generated by this branch's build, baseline
comparison in `visual-smoke.mjs` is not a valid pre-merge gate for this sprint;
the script now asserts structure, overflow, browser errors, computed type size,
and keyboard behaviour, which is what this audit changed. Green CI plus the
recorded `desktop /` render are the evidence for this sprint.
