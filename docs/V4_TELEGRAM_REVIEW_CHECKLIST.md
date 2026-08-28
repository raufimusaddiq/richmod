# V4 Telegram review category lane

- [x] Active categories are paginated in groups of eight
- [x] Category buttons use household-bound UUID callback tokens
- [x] Next/previous callbacks are bounded and stale pages are safe no-ops
- [x] Category callbacks bypass the general LLM pipeline
- [x] Selected categories are revalidated against the review household
- [x] Existing review confirmation and merchant-learning flow is preserved
- [x] Worker regression suite passes
- [ ] Merchant and description reply lanes
- [ ] Live multi-recipient Telegram verification
