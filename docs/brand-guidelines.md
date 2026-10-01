# Richmod brand guidelines

## Concept

**Retro Ledger.** Richmod is a household ledger, so the product borrows the
vocabulary of a well-kept paper book: cream stock, ruled lines, ink outlines,
offset shadows, and a display serif reserved for headings. The surfaces stay
calm and legible because the money is the loud part, not the chrome.

The single token source is `apps/web/app/globals.css`. This document describes
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
| Income | `--income` | `#315b3f` | Confirmed inflow |
| Expense | `--expense` | `#8b4437` | Outflow |
| Warning | `--warning` | `#73561e` | Review and incomplete data |
| Danger | `--danger` | `#913e40` | Destructive and failed states |
| Info | `--info` | `#3a5867` | Informational state |

### Rules

- Accent is the only brand colour used for interaction. There is one accent per
  viewport; income, expense, warning, danger, and info are reserved for financial
  meaning or an actionable system state.
- Accent budget per above-the-fold viewport: primary action, one key metric, and
  active navigation.
- Chart colour is drawn from `--chart-*`; the categorical ramp stays inside the
  teal, slate, olive, ochre, clay, and muted violet families and never repeats the
  income green.
- Text tokens must keep WCAG AA 4.5:1 against every surface token. This is
  enforced by `apps/web/tests/ui-audit-locks.test.mjs`; run it before shipping a
  palette change.

## Typography

| Role | Family | Weight | Notes |
| --- | --- | --- | --- |
| Display | Fraunces (variable, self-hosted) | 500-600 | `h1`, `h2`, `h3`, brand wordmark |
| Body and UI | Inter (variable, self-hosted) | 400-600 | Paragraphs, controls, tables, chart ticks |
| Numerals | Inter | 400-600 | `font-variant-numeric: tabular-nums` on every amount |

Both files are Latin subsets under `apps/web/public/fonts/`, loaded with
`display: swap` and a local sans-serif fallback. Keep the body floor at 11px;
`ui-audit-locks.test.mjs` fails on smaller text.

### Font Stack

Heading family: "Fraunces" (`--font-display` in CSS).
Body family: "Inter" (`--font-body` in CSS).

## Shape and depth

- Radius steps are tokens, not one repeated value: `--radius-xs` 8px,
  `--radius-sm` 12px, `--radius-md` 18px, `--radius-lg` 26px.
- Depth comes from a 2px ink outline plus an offset shadow (`--shadow`,
  `--shadow-float`). No blur-based glow.
- Controls keep a 44px minimum height and inputs stay at 16px on mobile.

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

| Asset | Use |
| --- | --- |
| `apps/web/app/icon.svg` | Favicon; teal mark, cream glyph |
| `apps/web/public/brand/richmod-evidence-flow.svg` | Landing evidence flow |
| `apps/web/public/brand/richmod-login-treeline.svg` | Login footer illustration |
| `apps/web/app/opengraph-image.js` | Social card |
