# V4 live bank-email compatibility checklist

This checklist records a read-only inspection of the connected Gmail inbox on
2026-08-28. It does not store message bodies, transaction values, message IDs,
or personal recipient data.

- [x] A recent bank notification was inspected in the connected inbox.
- [x] The message used an exact configured sender and passed DKIM and DMARC checks.
- [x] The message body was preserved from the available MIME representation.
- [x] Sender matching is based on the listener registry, not a bank-specific parser.
- [x] Unmatched or unauthenticated messages are rejected before extraction.
- [ ] A second directly authenticated sender should be configured and smoke-tested.
- [x] Live native-tool smoke test against the production `primary` model completed with a synthetic email and no database write.
