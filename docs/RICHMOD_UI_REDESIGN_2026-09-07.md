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

## Deliberate Limits

No new backend endpoints, financial calculations, persistence behavior, or
infrastructure were added. The design uses the data already supplied by the
existing application; more granular evidence indicators belong in a separate
product/API change only if the backend exposes a reliable field for them.
