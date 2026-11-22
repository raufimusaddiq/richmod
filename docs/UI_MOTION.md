# UI motion

Motion in Richmod explains a change; it never decorates. This page is the
contract for the animations in `apps/web/app/styles/15-motion.css` and the
scale they read.

## Scale

Defined once in `apps/web/app/styles/01-tokens-and-base.css` (the token source
`globals.css` imports; that file stays a pure import list, which
`apps/web/tests/ui-audit-locks.test.mjs` enforces):

| Token | Value | Used for |
| --- | --- | --- |
| `--motion-fast` | `120ms` | press, hover, and a dismissal on its way out |
| `--motion-base` | `180ms` | something enters: a row, a drawer, a dialog, a page |
| `--motion-slow` | `260ms` | a figure the user's own action just changed |
| `--ease-standard` | `cubic-bezier(.2, 0, 0, 1)` | starting and stopping under the user's hand |
| `--ease-out` | `cubic-bezier(.16, 1, .3, 1)` | something arriving |
| `--motion-chart` | `700ms` | a chart or progress bar drawing in |
| `--motion-chart-stagger` | `45ms` | per-row delay between category bars |
| `--ease-chart` | `cubic-bezier(.33, 1, .68, 1)` | the draw-in curve; a gentler ease-out so 700ms reads as 700ms |

No component hard-codes a duration or easing. The test above fails if a
literal duration appears next to the animation rules.

## Reduced motion

`apps/web/app/styles/15-motion.css` holds the single
`@media (prefers-reduced-motion: reduce)` rule. It collapses every animation
and transition in the app to one frame and stops loops after one pass. The
length is not zero on purpose: a zero-length animation may never fire
`animationend`, and the toast's exit and unmount are driven by that event, so a
`reduce` user would be left with a toast that never leaves. Nothing in the app
drives a timer from an animation duration.

## What animates, and why

| Motion | Where | Reason |
| --- | --- | --- |
| Row enter | `.transaction-row.is-new` | the row a create just returned is the answer to the button the user pressed |
| Result refresh | `.ledger-panel[data-refreshed]` | a filter re-ran the list; one fade on the panel marks the swap instead of animating every row |
| Toast in and out | `.toast` | the confirmation arrives and leaves instead of appearing and vanishing |
| Changed figure | `[data-updated]` on a wealth figure, or on the cycle position | the number the user's save or cycle choice changed is marked, and its `CountUp` tween runs underneath the mark |
| Drawer, sheet, dialog entry | `.detail-drawer`, `.admin-drawer`, `.mobile-more-panel`, `dialog[open]` | the panel used to cut into place |
| Page entry | `.route-fade .app-main` via `app/template.js` | a route change lands, under 200 ms, opacity only |
| Chart draw-in | Recharts bars and lines in `Charts.js`, `.history-bar`, `.where-stack`, `.category-bars-track`, `.home-share` | a series shows where its figures came from, once per dataset (see below) |
| Hero count-up | `CountUp` on the home hero and figures, the cycle KPI strip, the wealth position | the headline figure lands on its exact value instead of cutting in |

`data-updated` is set only by the code path the user triggered: saving or
correcting a wealth position, or choosing a cycle in Analytics. On Wealth it is
set per figure and only when the saved value differs from what the page showed
before the save, so an account that did not move never flashes. It is never set
on first paint or by a poll.

### One-shot markers

`data-refreshed` and `data-updated` are one-shot markers: they record one
action and are consumed when that action's render is on screen. Consumption is
a ref mutation in a `useEffect` that runs after the commit which painted the
marker, not a state update: a state update would re-render immediately and
remove the attribute while the animation is still running. The effect skips
while the page is loading, because the first commit after a filter or save is
the skeleton, before the panel or figures exist. The next unrelated render -
pagination, a create, a retry - then compares equal and stays still. There is
no timer, so the CSS duration remains the only source of timing.
`scripts/motion-one-shot-smoke.mjs` (`npm run test:motion`) drives the real
export to assert the first render animates and the next one does not.

### Chart draw-in and count-up

Recharts series take their motion props from `useChartMotion`, which keys on the
chart's data: a series animates when its data first mounts or changes, and the
hook turns the animation off when the series reports its draw finished (not on a timer, so a slow first paint cannot use it up), so hover, tooltip
and resize re-renders never replay it. CSS bars animate when their keyed element
mounts, which is also only on a dataset change. The category donut is the one
chart that stays static.

`CountUp` takes the already-formatted string (`money(...)`), tweens a display
number between the previous and the new value, and renders the original string
once it finishes, so the end state is always exact. While it runs, an
`aria-hidden` copy shows the tween and a visually hidden copy carries the final
string. It starts from the previous value on a change and from zero on mount.

Route entry (`[data-reveal]` and the view transition) fades and lifts the whole
content block over `--motion-page` while a chart's own draw-in and a counter
start at mount, so for the first ~400ms the figures grow inside a block that is
still fading in; both finish at their real values and neither hides the other.

Under `prefers-reduced-motion: reduce` none of this runs: the hook and the
counter see the preference through `useSyncExternalStore` (server and first
hydration render as reduced), and the CSS bar animations are declared only
under `no-preference`. `scripts/chart-motion-smoke.mjs`
(`npm run test:chart-motion`) samples every animation frame to assert both
modes.

## What is deliberately not animated

- **The dimmed backdrop.** It is a full-screen `backdrop-filter`; fading it
  re-blurs the page every frame on a phone and carries no information.
- **Drawer and modal exit.** React removes the node, so an exit needs presence
  tracking in every page that mounts one. The value is a short effect that
  delays the user's return to the list they were reading.
- **Count-ups outside the hero figures.** Only the headline figures tween
  (ADR-116); table cells, lists and drawers show their real value at once.
- **Press feedback on Save, Add and Pay.** `button:active` already acknowledges
  the press with a 1px translate and a `var(--motion-fast)` transition; a second
  mechanism would duplicate it.
- **Idle updates to the overview.** The overview figures count up when the page
  loads or its data is replaced, never on a timer or poll.
- **Scroll effects, parallax, idle motion.** Out of scope by decision.

## Performance

Every rule animates `opacity` and/or `transform` on an already-laid-out
element, except the changed-figure flash, which animates colour and shadow on a
single inline text box. No animation changes size, spacing or the DOM order (the chart draw-in clips or scales the bar itself, never its layout box), so
nothing reflows, a 44 px control keeps its hit area, and no control is disabled
or delayed while an animation runs. The service worker caches nothing, so the
cost is paid per route load; that is why there is one page-level fade and no
ambient motion.
