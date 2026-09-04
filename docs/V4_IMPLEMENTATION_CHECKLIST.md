# Richmod V4 implementation checklist

- [x] Required `emit_bank_transaction` tool exists
- [x] Responses and Chat Completions native calls normalize to one gateway call
- [x] Required tool choice, exactly-one call, unknown tools, prose, and invalid arguments fail closed
- [x] Nullable fields and whole-IDR validation exist
- [x] No bank extraction tool-result loop exists
- [x] Generic extractor uses the gateway abstraction only
- [x] Listener schema and owner API are in place
- [x] Real primary native-tool smoke test (synthetic, non-mutating)
- [x] Gmail sender/auth trust boundary and generic listener routing
- [x] Go policy matrix baseline exists
- [x] Partial extraction reaches Review Inbox and Telegram merchant/detail/category completion; matching initial review states are covered by worker tests
- [x] Full API/worker/web/Compose regression
