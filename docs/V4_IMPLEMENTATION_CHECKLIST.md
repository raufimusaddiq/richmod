# Richmod V4 implementation checklist

- [x] Required `emit_bank_transaction` tool exists
- [x] Responses and Chat Completions native calls normalize to one gateway call
- [x] Required tool choice, exactly-one call, unknown tools, prose, and invalid arguments fail closed
- [x] Nullable fields and whole-IDR validation exist
- [x] No bank extraction tool-result loop exists
- [x] Generic extractor uses the gateway abstraction only
- [x] Listener schema and owner API are in place
- [ ] Real primary/ocg/minimax native-tool smoke test
- [ ] Gmail sender/auth trust boundary and Jago shadow comparison
- [x] Go policy matrix baseline exists
- [ ] Partial extraction reaches Review Inbox and Telegram merchant/detail/category completion
- [ ] Full API/worker/web/Compose regression
