# V4 live bank-email compatibility checklist

This checklist records a read-only inspection of the connected Gmail inbox on
2026-08-28. It does not store message bodies, transaction values, message IDs,
or personal recipient data.

- [x] Recent native Jago mail was found in the inbox.
- [x] Jago mail used an exact service sender (`noreply@jago.com`).
- [x] Jago mail carried `dkim=pass` and `dmarc=pass` in Gmail authentication results.
- [x] Jago debit-card mail used an HTML body and included a localized IDR amount.
- [x] Jago transfer and RDN notifications were also observed, so generic policy must distinguish `TRANSFER` and `RDN` from spending.
- [x] A recent Jenius d-Card notification was found only inside a forwarded Gmail message.
- [x] The forwarded Jenius message exposed the original sender (`jenius_noreply@smbci.com`) only in the quoted body; its actual envelope sender was Gmail.
- [x] The forwarded Jenius message is rejected by exact sender matching and is not eligible for LLM extraction.
- [x] The worker now preserves nested `text/plain` MIME content when no HTML body exists, enabling a future direct Jenius listener without changing the trust boundary.
- [x] The legacy Jago parser now also requires DKIM and DMARC pass before it can run.
- [ ] A direct, authenticated Jenius message must be observed before enabling a Jenius listener in production.
- [ ] Live native-tool smoke test against `ocg/minimax-m3` remains gated and non-mutating.
