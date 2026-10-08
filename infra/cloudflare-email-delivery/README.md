# Cloudflare email delivery Worker

The second half of the email transport. The [ingress Worker](../cloudflare-email-ingress/README.md)
stores each raw message in R2 and enqueues its metadata. This Worker consumes
that queue, reads the raw message from R2, and posts it to the Richmod API.

```text
Cloudflare Email Routing -> ingress Worker -> R2 + Queue -> delivery Worker -> POST /finance/v1/email/inbond
```

The Worker contains no bank or provider rules. The signed recipient address
selects the household inside the Go API; raw MIME stays in R2 and the queue
carries metadata only.

## Configuration

Copy [`wrangler.toml.example`](wrangler.toml.example) to an untracked
`wrangler.toml` and adjust it.

| Name | Kind | Meaning |
| --- | --- | --- |
| `RICHMOD_EMAIL_RAW` | R2 binding | The bucket holding raw messages (`richmod-email-raw`). |
| `RICHMOD_INGRESS_URL` | variable | Your Richmod API origin plus the fixed path `/finance/v1/email/inbond`. The example uses a placeholder hostname; replace it. |
| `RICHMOD_INGRESS_SECRET` | secret | The HMAC signing secret. Set it with `wrangler secret put RICHMOD_INGRESS_SECRET`; never put it in `wrangler.toml`. It must be the same literal string as `EMAIL_INGRESS_HMAC_SECRET` in the Richmod API environment (see [`.env.example`](../../.env.example)). |

The runbook below documents this project's own production deployment, so it shows that
deployment's real hostnames. The example file uses a placeholder on purpose: use your own
hostname in `wrangler.toml`.

The queue consumer settings in the example are a batch of up to 10 messages, a
5 second batch timeout, up to 5 retries, and a dead-letter queue
(`richmod-email-delivery-dlq`) for messages that still fail.

## Deploy and verify

The deploy order, the forwarding setup, the smoke test, the HMAC request
contract and troubleshooting are in the
[Cloudflare email ingress runbook](../../docs/runbooks/cloudflare-email-ingress.md).
Only this Worker holds `RICHMOD_INGRESS_SECRET`; the ingress Worker holds no secret.
