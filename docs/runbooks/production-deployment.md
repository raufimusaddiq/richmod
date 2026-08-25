# Production deployment

## Public endpoint

The production hostname is `finance.investdx.biz.id`. DNS currently resolves to
the server public address. The host's existing Caddy container already owns ports
80 and 443, so this application must not start a second public Caddy instance.

The `compose.production.yaml` override joins `finance-api` and `finance-web` to
the existing external Docker network, `idx_default`. Add the reviewed site block
from `infra/caddy/finance.investdx.biz.id.caddy` to the host-managed Caddyfile
and validate it before reloading Caddy.

## Preconditions

- `idx_default` Docker network exists.
- The host Caddy configuration has the finance site block.
- Deployment secrets are injected from a restricted `.env` file or secret store.
- The working tree is updated `main`; deploy only from that worktree.

## Deploy

```text
docker compose --env-file /path/to/finance.env -f compose.yaml -f compose.production.yaml up -d --build
docker exec idx-caddy caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
docker exec idx-caddy caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
```

Verify `https://finance.investdx.biz.id` after deployment. Do not expose the
database, worker, API, or web ports directly in production.
