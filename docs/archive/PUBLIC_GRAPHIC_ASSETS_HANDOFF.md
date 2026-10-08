# Richmod Public Graphic Assets — Codex Handoff

## Intent

This branch adds two approved visual assets for the public Richmod experience. They are meant to enrich the existing landing and login pages without redesigning either page.

The current public experience and the current brand system remain authoritative. These assets are additive visual enrichment only.

## Assets

### 1. Evidence flow

Path:

`apps/web/public/brand/richmod-evidence-flow.svg`

Purpose:

- communicate Richmod's evidence-first product model visually;
- show email bank, Telegram, and documents flowing into Richmod;
- show the deterministic split between clear evidence becoming a ledger entry and unclear evidence asking the household for a decision;
- give the landing hero a stronger signature graphic instead of leaving a large empty product-art area.

The graphic already follows the current brand separation:

- dried-plum is used for Richmod identity;
- evergreen is reserved for confirmed/positive state;
- ochre is reserved for uncertainty/review;
- slate is informational;
- warm neutrals carry the editorial background.

### 2. Login treeline

Path:

`apps/web/public/brand/richmod-login-treeline.svg`

Purpose:

- add a quiet household/home motif to the lower portion of the desktop login context;
- make the login page feel intentional and warm rather than visually empty;
- remain subtle enough that the login form stays dominant.

This asset is decorative and should not communicate product state.

## Codex integration task

Use the two provided assets in the latest public Richmod UI.

Do not redraw them and do not replace them with generated blobs, stock illustrations, or a new visual style.

### Landing

Integrate `/brand/richmod-evidence-flow.svg` into the existing landing hero as the primary product graphic.

Preferred behavior:

- keep the existing hero copy, CTA, navigation, routing, and page structure;
- use the asset as the hero's signature product visual;
- replace or simplify redundant hero-only visual markup if the current `EvidenceBoard` duplicates the same story;
- do not remove later evidence/review sections merely because the hero now has a flow graphic;
- preserve semantic text elsewhere on the page;
- keep the asset responsive and never let it cause horizontal overflow;
- on mobile, stack it below hero copy with enough breathing room;
- do not crop important labels;
- do not give it a heavy browser-frame treatment.

Accessibility:

- the hero image is meaningful, so use concise alt text such as: `Alur Richmod dari bukti ke ledger atau keputusan manusia`;
- do not rely on the SVG text as the only explanation of the product flow; existing landing copy must remain.

### Login

Integrate `/brand/richmod-login-treeline.svg` into the existing login context area.

Preferred behavior:

- desktop: anchor it visually near the bottom of the left/context side, behind or below the trust copy;
- keep opacity restrained;
- never place it behind form inputs;
- do not reduce login-form contrast;
- mobile: either hide it or use a very shallow decorative strip if it remains visually useful;
- it must not increase the page height dramatically or create awkward empty scroll space.

Accessibility:

- this asset is decorative; use empty alt text or a CSS background with no semantic announcement.

## Design constraints

Do not:

- redesign landing or login;
- change the approved dried-plum brand system;
- change typography hierarchy;
- change auth behavior;
- change routes;
- invent new claims or integrations;
- introduce gradients, glassmorphism, neon, or generic fintech illustration;
- recolor semantic financial states into the brand accent;
- add another competing hero graphic on top of this one.

## Responsive verification

Review at minimum:

- landing: 1440x900, 1024x768, 390x844;
- login: 1440x900, 390x844.

Check:

- no horizontal overflow;
- no clipped SVG labels;
- no collision with navigation or CTA;
- hero still feels balanced on desktop;
- login card remains the primary action surface;
- treeline remains subtle;
- reduced-motion behavior is unaffected.

## Visual regression

Use the existing visual-regression workflow.

Update public landing/login baselines only after visual review of the integrated assets.

This is a visual-enrichment task, not a new redesign pass.
