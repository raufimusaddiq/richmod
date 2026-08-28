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
- [ ] Live multi-recipient Telegram verification
