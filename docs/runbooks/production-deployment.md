# Production deployment

## Public endpoint

The production hostname is `finance.investdx.biz.id`. DNS currently resolves to
the server public address. The host's existing Caddy container already owns ports
80 and 443, so this application must not start a second public Caddy instance.

The `compose.production.yaml` override joins `finance-api` and `finance-web` to
the existing external Docker network, `idx_default`. Add the reviewed site block
from `infra/caddy/finance.investdx.biz.id.caddy` to the host-managed Caddyfile
and validate it before reloading Caddy.

The production override also sets `WEB_ORIGIN` to the public HTTPS URL, causing
the API to issue `Secure` session cookies. Do not override it with an HTTP URL.

## Preconditions

- `idx_default` Docker network exists.
- The host Caddy configuration has the finance site block.
- Deployment secrets are injected from a restricted `.env` file or secret store.
- The working tree is updated `main`; deploy only from that worktree.

Required integration settings are `LLM_GATEWAY_BASE_URL`, `LLM_GATEWAY_API_KEY`,
`LLM_MODEL_TELEGRAM_EXTRACT`, `TELEGRAM_BOT_TOKEN`, and a random
`TELEGRAM_WEBHOOK_SECRET`. The worker joins `idx_default` only to reach the cloud
gateway; PostgreSQL remains on the internal network. Never commit the real values.

## Deploy

```text
docker compose --env-file /path/to/finance.env -f compose.yaml -f compose.production.yaml up -d --build
docker exec idx-caddy caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
docker exec idx-caddy caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
```

Before first login, run the one-time owner bootstrap and supply the owner's numeric
Telegram user ID with `--telegram-user-id`. Then register
`https://finance.investdx.biz.id/webhooks/telegram` with Telegram using the same
webhook secret. The bootstrap refuses to run after an owner exists.

Verify `https://finance.investdx.biz.id` after deployment. Do not expose the
database, worker, API, or web ports directly in production.
