# Cloudflare email transport

Deploy order:

1. Use already-provisioned R2 bucket `richmod-email-raw` and Queue `richmod-email-delivery`; DLQ is optional.
2. Copy both `wrangler.toml.example` files to untracked `wrangler.toml` files.
3. Set `RICHMOD_INGRESS_SECRET` with `wrangler secret put` on delivery worker. Use same value as Richmod `EMAIL_INGRESS_HMAC_SECRET`.
4. Deploy both workers.
5. Route `*@richmod.link` to `richmod-email-ingress`.
6. Provision household address in Richmod. Forward one real configured-sender email while status remains `PROVISIONED`.
7. Inspect raw `.eml` authentication headers, then set `EMAIL_INGRESS_TRUSTED_AUTHSERV_IDS` in existing `finance.env`. Do not infer this value.
8. Activate address only after transport and authentication evidence pass.

Workers contain no bank/provider rules. Recipient selects household inside Go. Raw MIME remains in R2; Queue carries metadata only.
