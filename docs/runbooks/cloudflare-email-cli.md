# Cloudflare email setup from the command line

This runbook sets up, checks and inspects Richmod's email transport from a
terminal instead of the Cloudflare dashboard. The resources, the request
contract and the security rules are in the
[Cloudflare email ingress runbook](cloudflare-email-ingress.md); this page only
turns that setup into commands.

Commands were checked against `cf` `1.0.0-beta.13`. `cf` is in beta, so confirm
a command with `--help` (for example `cf queues consumers create --help`) if it
fails.

## Which tool does what

Two Cloudflare CLIs are involved, and each has a job the other cannot do yet.

| Task | Tool | Why |
| --- | --- | --- |
| R2 bucket, Queues, Email Routing, read-only checks | `cf` | Covers the whole Cloudflare API, works in any directory, and does not read `wrangler.toml` |
| Deploying the two Workers | `npx wrangler deploy` | Both Workers are Wrangler projects (`wrangler.toml`). Do not run `cf deploy`, `cf dev` or `cf build` in them; Cloudflare warns these can fail or rewrite project files until the project is converted with `cf migrate` |
| Setting `RICHMOD_INGRESS_SECRET` | `npx wrangler secret put` | `cf` has no prompt-based single-secret command. `cf workers secrets update --text` would put the secret in shell history and the process list |
| Live Worker logs | `npx wrangler tail` | `cf` has no equivalent yet |

Converting the Workers with `cf migrate` would replace `wrangler.toml` with
`cloudflare.config.ts`. That is a separate decision and is out of scope here.

## Install and sign in

```bash
npm install --global cf
```

```bash
cf auth login
```

```bash
cf auth whoami
```

`cf` needs Node.js 22.18 or later and keeps its own login; it does not reuse a
Wrangler login. On a server without a browser, use `cf auth login --no-browser`.

For scripts and CI, skip the login and export an API token instead. Both `cf`
and Wrangler read the same variables:

```text
CLOUDFLARE_API_TOKEN   token limited to Workers Scripts, R2, Queues and Email Routing for this account and zone
CLOUDFLARE_ACCOUNT_ID  the account that owns the Workers, bucket and queues
CLOUDFLARE_ZONE_ID     optional default zone; accepts the zone ID or the domain (richmod.link)
```

Keep the token out of the repository and out of `finance.env`; Richmod itself
never needs it. Zone-scoped commands (Email Routing) take `--zone richmod.link`,
which overrides `CLOUDFLARE_ZONE_ID`.

Every command that writes accepts `--dry-run`, which shows the request without
sending it. Use it the first time you run each step.

## First-time setup

Run the steps in order. Each step lists what it creates and how to check it.

### 1. R2 bucket for raw messages

```bash
cf r2 buckets create --name richmod-email-raw
```

```bash
cf r2 buckets get richmod-email-raw
```

The bucket stays private: do not add a custom or managed public domain.

### 2. Delivery queue and dead-letter queue

```bash
cf queues create --queue-name richmod-email-delivery
```

```bash
cf queues create --queue-name richmod-email-delivery-dlq
```

```bash
cf queues list
```

`cf queues list` prints each queue's ID; the consumer and metrics commands
below take the ID, not the name. The DLQ name must match `dead_letter_queue`
in the delivery Worker's `wrangler.toml`. The example file uses
`richmod-email-delivery-dlq`; the main runbook records production as
`richmod-email-dlq`, so check which one exists before creating another.

### 3. Deploy the ingress Worker

```bash
cd infra/cloudflare-email-ingress
cp wrangler.toml.example wrangler.toml
npx wrangler deploy
```

This Worker holds no secret. It needs the `RICHMOD_EMAIL_RAW` bucket binding and
the `RICHMOD_EMAIL_DELIVERY` queue producer binding, both in the example file.

### 4. Deploy the delivery Worker and set its secret

```bash
cd infra/cloudflare-email-delivery
cp wrangler.toml.example wrangler.toml
```

Edit `wrangler.toml`: set `RICHMOD_INGRESS_URL` to your API origin with the
fixed path `/finance/v1/email/inbond`, and set the `[[queues.consumers]]`
values you want (see [Queue consumer settings](#queue-consumer-settings)).

```bash
npx wrangler deploy
```

```bash
npx wrangler secret put RICHMOD_INGRESS_SECRET --name richmod-email-delivery-worker
```

Wrangler prompts for the value, so it never appears on the command line. Use
the same literal string as `EMAIL_INGRESS_HMAC_SECRET` in the Richmod API
environment.

`wrangler deploy` also creates the queue consumer from `[[queues.consumers]]`.
Do not create it a second time with `cf queues consumers create`.

### 5. Email Routing for richmod.link

Enable Email Routing on the zone. This adds Cloudflare's MX and SPF records:

```bash
cf email-routing enable --zone richmod.link --dry-run
```

```bash
cf email-routing enable --zone richmod.link
```

Send every address on the domain to the ingress Worker with the catch-all rule.
Richmod generates household addresses dynamically, so there is no per-household
rule:

```bash
cf email-routing rules catch-all update --zone richmod.link \
  --matchers '[{"type":"all"}]' \
  --actions '[{"type":"worker","value":["richmod-email-ingress"]}]' \
  --dry-run
```

Run the same command without `--dry-run` to apply it. In PowerShell, pass the
JSON from a file instead (`--matchers @matchers.json --actions @actions.json`),
because PowerShell rewrites the quotes.

### 6. Continue in the main runbook

Provisioning a household address, Gmail forwarding, trusted authserv IDs and
activation are application steps; follow the
[ingress runbook](cloudflare-email-ingress.md#gmail-forwarding-setup) from there.

## Verify the live setup

These commands only read, so they are safe to run against production at any
time. They cover smoke-test steps 1 to 4 in the
[ingress runbook](cloudflare-email-ingress.md#smoke-test).

```bash
cf email-routing settings get --zone richmod.link
```

```bash
cf email-routing rules catch-all get --zone richmod.link
```

Expect Email Routing enabled, and a catch-all with matcher `all` whose action is
`worker` → `richmod-email-ingress`.

```bash
cf r2 buckets get richmod-email-raw
```

```bash
cf queues list
```

```bash
cf queues consumers list --queue-id <richmod-email-delivery ID>
```

Expect one `worker` consumer, `richmod-email-delivery-worker`, with the dead-letter
queue and settings from [Queue consumer settings](#queue-consumer-settings).

```bash
cf workers secrets list --worker richmod-email-delivery-worker
```

Expect `RICHMOD_INGRESS_SECRET` listed by name. The value is never shown.
`cf workers secrets list --worker richmod-email-ingress` should list nothing.

## Queue consumer settings

The delivery Worker's `[[queues.consumers]]` block is the source of truth:
every `wrangler deploy` reapplies it. A change made with
`cf queues consumers update` lasts only until the next deploy, so change
`wrangler.toml` instead.

| `wrangler.toml` key | Shown by `cf queues consumers list` as |
| --- | --- |
| `max_batch_size` | `settings.batch_size` |
| `max_batch_timeout` (seconds) | `settings.max_wait_time_ms` (milliseconds) |
| `max_retries` | `settings.max_retries` |
| `retry_delay` (seconds) | `settings.retry_delay` |
| `max_concurrency` | `settings.max_concurrency` |
| `dead_letter_queue` | `dead_letter_queue` |

The [example file](../../infra/cloudflare-email-delivery/wrangler.toml.example)
and the table in the ingress runbook disagree; see the note under
[Main Queue and DLQ](cloudflare-email-ingress.md#main-queue-and-dlq).

## Day-to-day inspection

Queue backlog and age:

```bash
cf queues metrics get <richmod-email-delivery ID>
```

Messages parked in the dead-letter queue. Peek shows message metadata without
removing anything:

```bash
cf queues messages peek <dead-letter queue ID> --batch-size 10
```

Live delivery logs, for example while a forwarded message is in flight:

```bash
npx wrangler tail richmod-email-delivery-worker
```

Replaying from the DLQ follows the ingress runbook: fix the cause, then replay
through the normal delivery path. Do not hand-craft messages with
`cf queues messages push`, and do not run `cf queues purge start` or
`cf queues messages purge` on the DLQ. Both remove the only pointer to evidence
that never reached Richmod.

## Changing something later

| Change | Command |
| --- | --- |
| New Worker code or `wrangler.toml` values | `npx wrangler deploy` in that Worker's folder |
| Rotate the HMAC secret | Set the new value in the API environment and with `npx wrangler secret put` in the same maintenance window; deliveries signed with the old value retry until both match |
| Point the catch-all elsewhere | `cf email-routing rules catch-all update` as in step 5, with `--dry-run` first |
| Turn email intake off | `cf email-routing rules catch-all update --zone richmod.link --matchers '[{"type":"all"}]' --actions '[{"type":"drop"}]'`. Messages are then dropped, not queued |

`cf email-routing disable` removes the zone's Email Routing DNS records. Use the
catch-all `drop` action above for a temporary stop instead.
