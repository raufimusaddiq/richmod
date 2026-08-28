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
`LLM_GATEWAY_PROTOCOL=responses`,
`LLM_MODEL_TELEGRAM_EXTRACT`, `LLM_MODEL_DOCUMENT_VISION`,
`LLM_MODEL_INSIGHTS`, `TELEGRAM_BOT_TOKEN`, and a random
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

## Runtime security and health

The production override applies memory and process limits and enables
`no-new-privileges` for application containers. Caddy limits request bodies to
12 MiB. The API additionally provides:

- `GET /healthz` for liveness;
- `GET /readyz` for database readiness;
- request IDs and structured access logs;
- same-origin checks on browser mutations;
- per-client login and webhook rate limits;
- authenticated owner-only `GET /api/v1/operations/status` for worker heartbeat,
  queue failures/backlog, Review Inbox backlog, and Gmail freshness.

An operational status of `degraded`, a worker heartbeat older than one minute,
or any failed jobs requires investigation. Inspect service logs using:

```text
docker compose --env-file /opt/family-finance/finance.env -f compose.yaml -f compose.production.yaml logs --since=30m api worker
```

Retry only after identifying the cause. A stale `RUNNING` job is reclaimed by
the worker after five minutes; normal retries use bounded exponential delay.

## Encrypted backups

Backups use restic and contain a verified custom-format PostgreSQL dump plus the
attachment volume. Production must use an off-host `RESTIC_REPOSITORY`; the
local repository default is for drills only. Retention is 14 daily, 8 weekly,
and 12 monthly snapshots.

1. Create `/opt/family-finance/restic-password` with a long random value and
   mode `0600`. Keep a separate recovery copy outside this server.
2. Set `RESTIC_REPOSITORY` and any repository credentials in the restricted
   production environment file.
3. Test one backup manually:

   ```text
   docker compose --env-file /opt/family-finance/finance.env -f compose.yaml -f compose.production.yaml --profile operations run --rm backup
   ```

4. Install `infra/systemd/family-finance-backup.service` and
   `infra/systemd/family-finance-backup.timer` under `/etc/systemd/system`, run
   `systemctl daemon-reload`, and enable the timer with `systemctl enable --now
   family-finance-backup.timer`.
5. Monitor `systemctl status family-finance-backup.timer` and the most recent
   service result. A successful daily service run is the backup-freshness
   signal.

Never print the restic password or repository credentials in logs.

### Current deployment state (2026-08-25)

- The daily systemd timer is enabled for 02:30 `Asia/Jakarta`, with randomized
  delay.
- The encrypted repository is currently the local `family-finance_backup_data`
  Docker volume.
- A production snapshot passed `pg_restore --list`, restic integrity checking,
  and an isolated database restore with matching financial table counts.
- Off-host repository configuration remains open. The local volume does not
  protect against server loss or host compromise and therefore does not complete
  production backup acceptance.

## Restore drill

Run the drill against an isolated PostgreSQL instance. The script refuses any
target database whose name does not end in `_restore_drill`.

```text
docker compose --env-file /opt/family-finance/finance.env -f compose.yaml -f compose.production.yaml --profile operations run --rm \
  --entrypoint /usr/local/bin/finance-restore-drill \
  -e RESTORE_DATABASE_URL='postgres://restore_user:REDACTED@restore-db:5432/family_finance_restore_drill?sslmode=disable' backup
```

After the restore succeeds, verify household, transaction, evidence, attachment,
and audit counts against the snapshot manifest, record the drill date, and
destroy the isolated restore database. Never point the drill at the canonical
database.

## Failure recovery

- API or web failure: retain PostgreSQL and attachment volumes, rebuild only the
  failed application service, then verify `/readyz` and HTTPS.
- Worker failure: restart the worker; stale jobs are reclaimed after five
  minutes and all handlers remain idempotent.
- Migration failure: do not force the version. Preserve logs and data, repair in
  a new migration, and redeploy.
- Database loss: stop API and worker writes, provision a clean PostgreSQL 17
  target, restore the latest verified off-host snapshot, validate counts, then
  switch application connectivity.
