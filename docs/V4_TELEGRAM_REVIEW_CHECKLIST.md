# V4 Telegram review category lane

- [x] Active categories are paginated in groups of eight
- [x] Category buttons use household-bound UUID callback tokens
- [x] Next/previous callbacks are bounded and stale pages are safe no-ops
- [x] Category callbacks bypass the general LLM pipeline
- [x] Selected categories are revalidated against the review household
- [x] Existing review confirmation and merchant-learning flow is preserved
- [x] Worker regression suite passes
- [x] Deterministic `review:edit`, `review:merchant`, and `review:description` callbacks
- [x] Bound merchant and description replies update only the exact review transaction
- [x] Merchant/detail updates write evidence and audit records
- [x] Missing merchant blocks category confirmation until completed
- [x] Bank partial extraction starts in the matching merchant/detail state before category selection
- [x] Live multi-recipient Telegram verification; production evidence shows active linked household identities received the same bound review and shared review state resolves after one member action.

## Missing reply metadata (2026-10-08)

When an incoming message lacks `reply_to_message`, the bounded route request
includes eligible chat review count and, for one review, its type, conversation
state, and awaiting field (merchant, date, or category). Canonical IDs stay
server-only. An accepted
`REVIEW_INTERACTION` reaches the existing bound workflow instead of only listing
reviews. Unrelated requests keep their own route; multiple reviews never select
a target implicitly. Exact replies retain precedence. A rejected route grants
no mutation authority; review answers ask for Telegram Reply rather than claiming
a temporary outage. OWNER and MEMBER use the same household-scoped path.
