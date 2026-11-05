# Production deployment

## Public endpoint

The production hostname is `finance.musaddiqr.com`. DNS currently resolves to
the server public address. The host's existing Caddy container already owns ports
80 and 443, so this application must not start a second public Caddy instance.

The `compose.production.yaml` override joins `finance-api` to the existing
external Docker network, `idx_default`; the Go binary serves both the API and the
embedded static frontend, so there is no separate web container. Add the reviewed
site block from `infra/caddy/finance.musaddiqr.com.caddy` to the host-managed
Caddyfile and validate it before reloading Caddy. The block proxies every path to
`finance-api:8080`; the Go handler owns the per-route `Cache-Control` values.

The production override also sets `WEB_ORIGIN` to the public HTTPS URL, causing
the API to issue `Secure` session cookies. Do not override it with an HTTP URL.

## One-time cleanup after ADR-056

The `web` service and its image no longer exist, and the release script does not
remove orphans. After the first deploy of the single-container layout is healthy,
delete the leftover web container and its unused release images on the host:

```bash
docker ps --filter 'name=family-finance-web' --format '{{.Names}}' | xargs -r docker rm -f
docker image ls --format '{{.Repository}}:{{.Tag}}' | grep 'richmod-web:sha-' | xargs -r docker image rm
```

The `edge` network stays: the API uses it for outbound OSS and LLM gateway
requests. Do not run `docker system prune`.

## Preconditions

- `idx_default` Docker network exists.
- The host Caddy configuration has the finance site block.
- Deployment secrets are injected from a restricted `.env` file or secret store.
- The working tree is updated `main`; deploy only from that worktree.

Required integration settings are `LLM_GATEWAY_BASE_URL`, `LLM_GATEWAY_API_KEY`,
`LLM_GATEWAY_PROTOCOL=responses`,
`LLM_MODEL_TELEGRAM_EXTRACT`, `LLM_MODEL_BANK_EXTRACT`, `LLM_MODEL_DOCUMENT_VISION`,
`LLM_MODEL_INSIGHTS`, `TELEGRAM_BOT_TOKEN`, and a random
`TELEGRAM_WEBHOOK_SECRET`. The worker joins `idx_default` only to reach the cloud
gateway; PostgreSQL remains on the internal network. Never commit the real values.

`JUDGMENT_MODEL` is **required** in production. Jev/System One owns bounded
semantic mutation authority (ADR-038), so the worker refuses to start when the
model is unset, which prevents silently disabling Jev-owned semantics. It uses
the same LiteRouter URL/key and `/v1/systemone`; `JUDGMENT_TIMEOUT_MS` is capped
by the worker. Non-production environments opt out explicitly by setting
`JUDGMENT_MODE=disabled-dev`.

Configuration and availability are different states. A configured-but-unreachable
Jev degrades per turn: READ questions may still use a generative READ-only agent,
while mutation requests are refused instead of falling back to generative
semantic authority. Do not describe an outage as a reason to unset
`JUDGMENT_MODEL`.

Before trusting a new LiteRouter or Jev deployment, run the opt-in live residual
category canary through Richmod → LiteRouter `/v1/systemone` → TypeSafe and back
through the Richmod decoder. It submits synthetic receipt evidence with known
merchant, amount, and date, asks only for category among server-owned household-
style choices, and expects `makanan-minuman`. It makes one non-canonical decision
and no database write. The check is skipped by default because it consumes live
provider credits; run it from an approved secret-bearing environment:

```bash
cd apps/worker && SYSTEMONE_SMOKE_BASE_URL=<literouter base url> \
  SYSTEMONE_SMOKE_LITEROUTER_KEY=<literouter client key> \
  go test ./internal/judgment/systemone/ -run TestRealLiteRouterSystemOneSmoke -v
```

`SYSTEMONE_SMOKE_MODEL` is optional and defaults to `typesafe/jev-latest`, the
same model id production sets in `JUDGMENT_MODEL`. The smoke
asserts the provider accepts the native question schema, returns a real
versioned model id, and chooses the residual category supported by the evidence
from server-supplied criteria. Supply only the LiteRouter client key; no upstream
provider key belongs in Richmod.

Run the smoke from a host that shares a Docker network with LiteRouter and use
its internal address (`http://9router:20128/v1`), not the public hostname. The
worker reaches LiteRouter directly over the container network by design; sending
a bounded decision out through Cloudflare and back into the same host would add
a public round trip and a needless failure surface for no benefit. The internal
name only resolves inside those networks, so a shell on the host itself cannot
reach it:

```bash
docker run --rm --network idx_default -v "$PWD:/src" \
  -v richmod-gomod:/go/pkg/mod -w /src/apps/worker \
  --env-file /opt/family-finance/finance.env \
  -e SYSTEMONE_SMOKE_BASE_URL=http://9router:20128/v1 \
  golang:1.27-alpine sh -c '
    export SYSTEMONE_SMOKE_LITEROUTER_KEY="${LLM_GATEWAY_API_KEY}"
    go test ./internal/judgment/systemone/ -run TestRealLiteRouterSystemOneSmoke -v'
```

The smoke reports the concrete version the alias resolved to (for example
`jev-1.13.0`). `typesafe/jev-latest` is intentionally an alias; the concrete
version is what belongs on a stored `judgment_decision` row, so a decision stays
reproducible after the alias advances (PRD §18).

Off-host storage requires `OSS_ENDPOINT`, `OSS_REGION`, `OSS_BUCKET`, `OSS_PREFIX`,
`OSS_ACCESS_KEY`, and `OSS_SECRET_KEY`. API and worker mirror private attachments
under `<prefix>/attachments`; backup stores encrypted restic data under
`<prefix>/backups/restic`. Never expose OSS credentials to web or browser code.

## Deploy

```text
docker compose --env-file /path/to/finance.env -f compose.yaml -f compose.production.yaml up -d --build
docker exec idx-caddy caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
docker exec idx-caddy caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
```

## GitHub release-image and deployment workflow

Production builds must move off the host. `Release Images` runs after a
successful `main` CI run and publishes immutable API, worker, web, and migration
images to GHCR tagged `sha-<full-main-commit>`. It does not deploy.

`Deploy Production` is manual-only and uses the GitHub `production` Environment;
configure required reviewers there before its first use. It accepts only a commit
already reachable from `main`. The job sends the ephemeral GitHub token over SSH
stdin for one GHCR login, pulls exact immutable images, runs the one-shot
migration with `docker compose run --rm`, restarts
the application with `--no-build`, checks public health, retains the current and
previous release image on the host, removes older Richmod release tags, and
logs out of GHCR. If the previous running image cannot be identified, cleanup
is skipped. GHCR package versions are not deleted by the host deploy script.
Runtime secrets remain only in `/opt/family-finance/finance.env`.

Configure these GitHub Environment secrets without storing them in the repo or
`finance.env`:

```text
PROD_DEPLOY_HOST
PROD_DEPLOY_USER
PROD_DEPLOY_SSH_KEY
PROD_DEPLOY_KNOWN_HOSTS
```

The server deployment worktree must remain clean, be checked out on `main`, and
have Docker Compose access. GHCR packages must remain linked to this repository
so the ephemeral repository token can pull them. If package access changes,
repair package repository access; do not add registry credentials to the runtime
environment file.

### Standard release, reclaim, and approval sequence

Use this order for every normal production release:

The complete sprint preflight, test matrix, integration, cleanup, and handoff
checklist is in [the sprint delivery runbook](sprint-delivery.md).

1. Merge the verified branch to `main`; wait for `main` CI and `Release Images`
   to succeed.
2. After immutable `sha-<main-commit>` images exist in GHCR, reclaim only safe
   local artifacts: merged linked worktrees, their local branches, disposable
   build/dependency caches, and dangling test artifacts.
3. Do not wait for production approval before that reclaim. The later deployment
   pulls the immutable registry images and does not require those artifacts.
4. Submit `Deploy Production` for the exact verified SHA. The human approver
   approves it in GitHub's `production` Environment.
5. The workflow deploys, migrates if needed, and verifies `/healthz` and
   `/readyz`. Check the workflow result afterward.

Never reclaim production containers, PostgreSQL/attachment/backup volumes,
`/opt/family-finance/finance.env`, or images currently used by production.

`infra/deploy/post-deploy.sh` runs health and readiness checks before removing
older host-local `sha-*` tags for the four Richmod release repositories. It
keeps the deployed tag and the tag observed on the previous running service;
failed removals are warnings and do not turn a healthy deployment into a
failed deployment. GHCR cleanup remains a separate registry-retention task.

Rollback is a new manual `Deploy Production` run using a previously verified
main SHA whose four images exist in GHCR. Never use `docker compose up --build`
for a release-image deployment.

### Migration 00046 rollback caveat

Migration `00046` is forward-schema compatible and additive. Rolling application
images back to a verified SHA is permitted only while that SHA tolerates the
extended schema and legacy `review_request.transaction_id` rows. Do not roll the
schema back or delete wealth observations, savings intent, or residual-review data.
If a post-00046 application rollback is needed, retain the schema, deploy the
verified image, verify compatibility in logs and smoke checks, then repair forward
with a new migration if required.

### Post-deploy wealth and residual smoke checks

After `/healthz` and `/readyz` pass, use an owner session in one test household to:

1. Confirm an existing cashflow transaction still renders and a confirmed transfer
   remains classified as `INTERNAL_TRANSFER`, not an expense.
2. Record or view one wealth snapshot; confirm it is displayed as a point-in-time
   observation and does not create a cashflow transaction.
3. Confirm a savings-intent transfer retains its purpose without historical
   inference changing unrelated transactions.
4. Close or catch up one eligible salary cycle; confirm any residual creates or
   reuses one review item/request, never a synthetic transaction.
5. Re-run the residual trigger/catch-up path; confirm no duplicate active residual
   review appears.

Monitor the worker after deployment and after the smoke checks. The owner-only
`GET /api/v1/operations/status` must show a current heartbeat, no failed jobs,
and no unexpected queue backlog for that owner's household. Its job counts are
household-scoped (ADR-058); check the platform-wide queue as Super Admin on
`GET /api/v1/admin/overview`. Investigate residual-generation or wealth-related
job failures before considering the release complete.

### Telegram webhook verification

Telegram does not follow redirects. If the public hostname or its Caddy host block
changes, the registered webhook keeps answering `301 Moved Permanently`, so every
inbound update is rejected — and because `pending_update_count` stays at `0`
(rejected updates are not queued), inbound loss is invisible to `/healthz`,
`/readyz`, and the operations status endpoint.

After the health checks pass, with `WEB_ORIGIN`, `TELEGRAM_BOT_TOKEN`, and
`TELEGRAM_WEBHOOK_SECRET` loaded from the deployment environment file, verify the
registered URL:

```bash
scripts/set-telegram-webhook.sh --check
```

It exits non-zero when the registered URL is not `${WEB_ORIGIN}/webhooks/telegram`.
On failure, re-register it with the same script and no flag:

```bash
scripts/set-telegram-webhook.sh
```

Both modes refuse to run when any of the three variables is unset, keep
`allowed_updates` at `["message","callback_query"]`, never drop pending updates,
and never print the token or secret. `infra/deploy/post-deploy.sh` runs `--check`
after its health checks and warns loudly — without failing the deploy — when the
webhook is stale.

Before first login, run the one-time owner bootstrap and supply the owner's numeric
Telegram user ID with `--telegram-user-id`. Then register the webhook with
`scripts/set-telegram-webhook.sh`, which derives the URL from `WEB_ORIGIN` and
uses the same webhook secret. The bootstrap refuses to run after an owner exists.

### Migration 00087 pre-check (same-household foreign keys)

Migration `00087` (ADR-058) adds 61 same-household foreign keys and validates
every existing row in one transaction. It briefly holds write locks on the
ledger, review, email and Telegram tables until it commits, so deploy it in a
quiet window; on household-scale data it takes well under a second. If any
existing row references another household's data, the migration fails and the
deploy stops: it never repairs data. Before the deploy that first includes
`00087`, run the read-only pre-check against production:

```bash
docker compose --env-file /opt/family-finance/finance.env -f compose.yaml -f compose.production.yaml \
  exec -T db sh -c 'psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -At' \
  < scripts/check-cross-household-references.sql
```

Every line must end in `|0`. A non-zero count names the constraint and the
number of cross-household rows; stop and investigate those rows with the owner
before deploying. Do not delete or rewrite them to make the migration pass.

### Adding another household

One deployment hosts any number of households; `household_id` isolates them and
the database rejects cross-household references (ADR-058). Create each further
household with its own new owner, supplying the password on standard input:

```bash
printf '%s\n' '<owner-password>' \
  | docker compose --env-file /opt/family-finance/finance.env -f compose.yaml -f compose.production.yaml \
      exec -T api /bootstrap --new-household \
      --email second-owner@example.com --name 'Second Owner' \
      --household 'Second Household' --telegram-user-id <numeric-id>
```

It creates the household, the `OWNER`, the optional Telegram identity and the
default categories in one audited transaction. It refuses an email that already
has a user and a Telegram ID that is already linked: a user is never moved
between households (ADR-035). The new owner then provisions the household's own
email ingress address and invites members from the dashboard.

Verify `https://finance.musaddiqr.com` after deployment. Do not expose the
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
  the household's own queue failures/backlog, and Review Inbox backlog
  (platform-wide queue health is the Super Admin overview).

An operational status of `degraded`, a worker heartbeat older than one minute,
or any failed jobs requires investigation. Inspect service logs using:

```text
docker compose --env-file /opt/family-finance/finance.env -f compose.yaml -f compose.production.yaml logs --since=30m api worker
```

Retry only after identifying the cause. A stale `RUNNING` job is reclaimed by
the worker after five minutes; normal retries use bounded exponential delay.

## Auto-confirm kill-switches

The PRD §33 rollout-safety switches let an operator stop one source from writing
canonical ledger money without a person, without touching any other source.
Every switch defaults to enabled; only an explicit negative value disables it, so
an unset or mistyped variable never silently changes behavior.

| Variable | Source | Effect when disabled |
| --- | --- | --- |
| `RICHMOD_AUTOCONFIRM_BANK_CATEGORY` | Bank Email | A merchant expense with a learned or decided category parks in review with the category proposed, instead of confirming |
| `RICHMOD_AUTOCONFIRM_RECEIPT` | Receipt | Skips residual Jev category rescue; a clear new receipt parks in review with its extraction proposed |
| `RICHMOD_AUTOCONFIRM_SCREENSHOT` | Screenshot | Skips the selective Jev category batch; screenshot rows park in review instead of confirming |
| `RICHMOD_AUTOCONFIRM_TELEGRAM` | Telegram | A complete generative extraction parks in review with its facts proposed, instead of confirming directly; no Jev replay is added |

Setting the variable to `0`, `false`, `off`, `no` or `disabled` (any case)
disables the switch; any other value keeps it on. Disabling is a worker-only
change, so restart the worker after editing `finance.env`:

```text
docker compose --env-file /opt/family-finance/finance.env -f compose.yaml -f compose.production.yaml up -d worker
```

Disabling a source does not lose data: the evidence is still processed, the
proposal is still written, and the card still carries the proposed category, so
re-enabling the switch later needs no backfill.

## Semantic canary corpus

PR #120 showed that prompt-text regression tests do not protect against semantic
provider drift: the prompt can be byte-identical while the model behind the alias
starts reading the same notification email differently. The PRD §23 canary corpus
in `apps/worker/internal/bankemail/canary_corpus_test.go` exercises the live
provider on the email shapes that matter and asserts the bounded outcome.

It is not part of CI. Run it:

- manually before trusting a new gateway or model alias;
- in staging after a provider change;
- before a production promotion that touches bank-email semantics;
- periodically, where operationally practical.

```bash
docker run --rm --network idx_default -v "$PWD:/src" \
  -v richmod-gomod:/go/pkg/mod -w /src/apps/worker \
  --env-file /opt/family-finance/finance.env \
  -e RICHMOD_SEMANTIC_CANARY=1 \
  golang:1.27.0 go test ./internal/bankemail/ -run SemanticCanary -v -timeout 900s
```

It reads `LLM_MODEL_BANK_EXTRACT` (falling back to `LLM_MODEL_TELEGRAM_EXTRACT`)
and uses the same LiteRouter URL/key as the worker. Point the gateway base URL at
LiteRouter's internal address (`http://9router:20128/v1`) so the check does not
route back through the public edge.

Cases covered: a completed transaction carrying a security footer; a
support-phone-only footer; a transaction-history link; a promo/cashback email; a
monthly statement; two plausible amounts; an internal investment move; and an
incoming credit. The last two assert the deterministic `SPENDING_ONLY` verdict on
top of the extraction, so the model and the Go decision boundary are checked
together.

A failure names the case and prints the observed facts. Treat it as a
pre-promotion signal: if a real transaction email stops being read as a
transaction, do not ship the promotion until the cause is understood. Assertions
stay on bounded facts (kind, direction, presence of a known amount) so ordinary
wording drift does not fail the check while a real semantic regression does.

## Encrypted backups

Backups use restic and contain a verified custom-format PostgreSQL dump plus the
attachment volume. Production must use an off-host `RESTIC_REPOSITORY`; the
local repository default is for drills only. Retention is 14 daily, 8 weekly,
and 12 monthly snapshots.

Retention groups snapshots by tag (`forget --group-by tags`), and every backup
pins `--host family-finance` and a fixed staging path so consecutive runs share
a parent. Before #385 each run had a random container hostname and `mktemp`
path, so each snapshot was its own group and nothing was ever forgotten. The
first run after that fix applies the policy to the whole backlog at once; take a
`restic forget --dry-run --group-by tags ...` listing before deploying it.

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

The backup image copies `infra/backup/*.sh` at build time, and the systemd unit
only runs `compose run`, which does not rebuild an existing image. After a
change to those scripts, rebuild it on the host:

```bash
docker compose --env-file /opt/family-finance/finance.env -f compose.yaml \
  -f compose.production.yaml --profile operations build backup
```

If the change touches retention (`forget` flags or grouping), rebuild only after
the owner has approved the `restic forget --dry-run` listing of snapshots the
next run will remove. The first rebuild after #385 removes 36 snapshots
(2026-08-30 → 2026-10-12; listing in PR #387).

`backup.sh` holds an exclusive `flock` on its staging directory, so a manual run
that overlaps the timer exits with "another backup run holds …" instead of
racing it; rerun it after the timer finishes.

If a run fails with "repository is already locked", `backup.sh` clears stale
locks itself; a lock held by a live process is never removed.

Never print the restic password or repository credentials in logs.

### Current deployment state (2026-08-30)

- Daily systemd timer runs at 02:30 `Asia/Jakarta`, with randomized delay.
- Encrypted repository is OSS-backed under `richmod/backups/restic`.
- Snapshot `d30d4eb6` passed `pg_restore --list`, restic integrity checking, and
  a PostgreSQL 17 disposable restore drill.
- Existing attachment cache was backfilled to `richmod/attachments`.

## Attachment backfill

Run after enabling OSS and before treating host loss as recoverable. The command
is idempotent. Cloudeka rejects AWS CLI streaming checksums, so require the two
checksum compatibility variables below.

```text
AWS_REQUEST_CHECKSUM_CALCULATION=when_required \
AWS_RESPONSE_CHECKSUM_VALIDATION=when_required \
aws --endpoint-url "$OSS_ENDPOINT" --region "$OSS_REGION" \
  s3 sync /var/lib/docker/volumes/family-finance_attachment_data/_data \
  "s3://$OSS_BUCKET/$OSS_PREFIX/attachments" --no-progress
```

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

## Cloudflare generic email ingress (two deploys)

Deploy 1 adds the generated `richmod.link` recipient and keeps Gmail runtime
available for migration. Deploy with `EMAIL_INGRESS_HMAC_SECRET` and
`EMAIL_INGRESS_DOMAIN`; `EMAIL_INGRESS_TRUSTED_AUTHSERV_IDS` may remain unset
during the `PROVISIONED` bootstrap. The public Caddy allowlist must route only
`POST /finance/v1/email/inbond` on `api.musaddiqr.com` to `finance-api:8080`.
After receiving a real forwarded `.eml`, inspect its authentication headers and
set the observed trusted auth-service IDs before activating any household. An
`ACTIVE` address without this configuration fails closed. Provisioned deliveries
are transport verification only; activation atomically sets the address `ACTIVE`
and Gmail `DISCONNECTED`. DLQ remains optional and is not a Deploy 1 blocker.

Production cutover completed on 5 September 2026. A real forwarded Jago
debit-card notification traversed Cloudflare ingress through the existing bank
email processor and produced a confirmed transaction. Deploy 2 removes Gmail
OAuth/API/PubSub/watch/history runtime code, configuration, jobs, and tables.
Historical source/evidence rows remain. Gmail may still forward mail, but
Richmod no longer connects to Google APIs. External Google OAuth/PubSub resource
cleanup remains an operational follow-up.

- API or web failure: retain PostgreSQL and attachment volumes, rebuild only the
  failed application service, then verify `/readyz` and HTTPS.
- Worker failure: restart the worker; stale jobs are reclaimed after five
  minutes and all handlers remain idempotent.
- Migration failure: do not force the version. Preserve logs and data, repair in
  a new migration, and redeploy.
- Database loss: stop API and worker writes, provision a clean PostgreSQL 17
  target, restore the latest verified off-host snapshot, validate counts, then
  switch application connectivity.
