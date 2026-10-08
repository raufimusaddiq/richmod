# Richmod — Cloudflare Email Ingress in Two Deploys
## Product Requirement, Modification Plan, Generated Recipient Address, and Gmail Sunset

**Repository:** `raufimusaddiq/richmod`  
**Implementation target:** Codex  
**Backend:** Go  
**Database:** PostgreSQL  
**Reviewed base:** `main` at `5b7b02c2eb950d81c45711fc002ce18b3c210745`  
**Date:** 2026-09-04  
**Delivery model:** exactly **2 application deploys**

> This migration is intentionally split into two deploys.
>
> **Deploy 1** introduces a fully deployable Cloudflare forwarded-email flow, including backend-generated recipient addresses under `richmod.link`, while keeping the current Gmail implementation available until explicit household cutover.
>
> There is **no shadow financial ingestion** and no dual-active provider. Before cutover Gmail is active; after cutover forwarded-email is active and Gmail is disabled for that household.
>
> **Deploy 2** permanently removes Gmail OAuth/API/Watch/PubSub implementation, Gmail jobs, Gmail configuration, dependencies, and Gmail-only database state after the new flow has been verified in production.

---

# 0. Codex execution rules

Before changing files:

1. Read `AGENTS.md`.
2. Read:
   - `docs/RICHMOD_PRODUCT_ALIGNMENT_V2.md`
   - relevant bank-email ADRs
   - `docs/MVP_COMPLETION_CHECKLIST.md`
3. Re-inspect current `main`.
4. Follow the repository-required branch/worktree flow.
5. Add an ADR for the new inbound-email architecture.
6. Do not commit secrets.
7. Run relevant tests before completion.

Suggested worktree for Deploy 1:

```bash
git fetch origin
git checkout main
git pull --ff-only

git worktree add \
  -b feat/cloudflare-email-ingress \
  ../family-finance-worktrees/cloudflare-email-ingress \
  main
```

Suggested worktree for Deploy 2:

```bash
git fetch origin
git checkout main
git pull --ff-only

git worktree add \
  -b refactor/remove-gmail-ingestion \
  ../family-finance-worktrees/remove-gmail-ingestion \
  main
```

---

# 1. Final product architecture

After Deploy 2:

```text
Bank / financial institution
    ↓
Primary Gmail account
    ↓
Gmail filter
    ↓
Forward only email from configured financial senders/listeners
    ↓
h_<opaque-token>@richmod.link
    ↓
Cloudflare Email Routing
    ↓
richmod-email-ingress
    ├── raw RFC822/MIME → R2 richmod-email-raw
    └── metadata → Queue richmod-email-delivery
                         ↓
                richmod-email-delivery-worker
                         ↓ HTTPS + HMAC
POST https://api.investdx.biz.id/finance/v1/email/inbond
                         ↓
                    Richmod Go API
                         ↓
              deterministic trust checks
                         ↓
                    source_event
                         ↓
                  bank_email_event
                         ↓
                 PROCESS_BANK_EMAIL
                         ↓
          existing generic native-tool pipeline
                         ↓
          proposal / reconciliation / review
                         ↓
                    canonical ledger
```

Steady state contains:

```text
NO Gmail OAuth
NO Gmail API mailbox access
NO Gmail watch
NO Gmail Pub/Sub
NO Gmail history cursor
```

Gmail remains only as the user's own mail client and forwarding source.

The ingress transport is **not bank-specific and not institution-specific**. Cloudflare only delivers authenticated raw email. Richmod resolves the household from the generated recipient and resolves the allowed sender through the existing `bank_email_listener` records. Adding another supported bank or financial sender must require configuration/data, not a new ingress code branch.

---

# 2. Core migration rule

For one household, only one ingestion provider may create financial bank-email events at a time.

Allowed states:

```text
STATE A — before cutover

Gmail API integration       = ACTIVE
Cloudflare recipient        = PROVISIONED
Cloudflare financial ingest = DISABLED
```

```text
STATE B — after cutover, before Deploy 2

Gmail API integration       = DISCONNECTED
Cloudflare recipient        = ACTIVE
Cloudflare financial ingest = ACTIVE
```

```text
STATE C — after Deploy 2

Gmail API code              = REMOVED
Cloudflare recipient        = ACTIVE
Cloudflare financial ingest = ACTIVE
```

Not allowed:

```text
Gmail ACTIVE
+
Cloudflare ACTIVE
```

for the same household.

This avoids duplicate source events across two providers.

---

# 3. Product goals

The change MUST achieve:

- a provider-agnostic inbound email transport;
- generated per-household recipient addresses;
- safe household routing;
- HMAC-authenticated Cloudflare delivery;
- preserved raw email evidence in R2;
- existing generic bank-email processing reused;
- idempotent queue retries;
- explicit provider cutover;
- full Gmail removal in Deploy 2;
- no institution-specific Cloudflare logic;
- no second financial pipeline.

---

# 4. Cloudflare infrastructure contract

Expected external resources:

```text
richmod.link
│
├── Email Routing catch-all
│   └── *@richmod.link
│       └── richmod-email-ingress
│
├── Worker
│   └── richmod-email-ingress
│
├── R2
│   └── richmod-email-raw
│
├── Queue
│   └── richmod-email-delivery
│
└── Worker
    └── richmod-email-delivery-worker
```

Delivery worker target:

```text
https://api.investdx.biz.id/finance/v1/email/inbond
```

Cloudflare delivery worker config:

```text
RICHMOD_INGRESS_URL=https://api.investdx.biz.id/finance/v1/email/inbond
RICHMOD_INGRESS_SECRET=<secret>
```

Richmod API must use the same HMAC secret:

```text
EMAIL_INGRESS_HMAC_SECRET=<same secret>
```

---

# 5. Cloudflare → Richmod HTTP protocol

## Endpoint

```http
POST /finance/v1/email/inbond
Content-Type: message/rfc822
```

This endpoint is machine-to-machine.

Do not require a user session.

Authenticate using HMAC.

## Headers

```text
X-Richmod-Timestamp
X-Richmod-Recipient
X-Richmod-Envelope-From
X-Richmod-Content-SHA256
X-Richmod-Signature
X-Richmod-Message-ID
X-Richmod-Object-Key
```

Body:

```text
raw RFC822/MIME bytes
```

## HMAC canonical form

```text
timestamp
+ "\n"
+ recipient
+ "\n"
+ envelopeFrom
+ "\n"
+ sha256
```

Algorithm:

```text
HMAC-SHA256
```

Output:

```text
lowercase hex
```

Go:

```text
crypto/hmac
crypto/sha256
encoding/hex
```

Compare using:

```go
hmac.Equal(...)
```

## Timestamp

Accept:

```text
±5 minutes
```

from current UTC Unix seconds.

## HTTP semantics

| Condition | Status |
|---|---:|
| accepted | 202 |
| duplicate | 200/202 |
| recipient not active | 202 |
| unknown recipient | 202 |
| unmatched sender | 202 |
| authentication rejected | 202 |
| malformed MIME | 400 |
| SHA mismatch | 400 |
| invalid HMAC | 401/403 |
| transient DB failure | 500 |
| unavailable | 503 |

Unknown recipient must not reveal whether the address exists.

---

# 6. Database model

Use new migrations. Never modify old migration files.

`BANK_EMAIL` remains the current domain purpose/source type because the downstream Richmod financial pipeline is `bank_email_listener` → `bank_email_event` → `PROCESS_BANK_EMAIL`. This is a domain classification, not a hardcoded bank/provider choice. The transport and MIME ingestion code must remain generic.

## 6.1 `email_ingress_address`

Recommended:

```sql
CREATE TABLE email_ingress_address (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    household_id UUID NOT NULL REFERENCES household(id),

    local_part TEXT NOT NULL,

    purpose TEXT NOT NULL
        CHECK (purpose IN ('BANK_EMAIL')),

    provider TEXT NOT NULL
        CHECK (provider IN ('CLOUDFLARE_EMAIL')),

    status TEXT NOT NULL
        CHECK (status IN ('PROVISIONED','ACTIVE','DISABLED')),

    created_by_user_id UUID REFERENCES "user"(id),

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at TIMESTAMPTZ,
    disabled_at TIMESTAMPTZ,
    last_received_at TIMESTAMPTZ,

    CONSTRAINT email_ingress_local_part_format
        CHECK (local_part ~ '^h_[a-f0-9]{32}$')
);

CREATE UNIQUE INDEX email_ingress_address_local_part_unique
ON email_ingress_address(local_part);

CREATE UNIQUE INDEX email_ingress_address_current_household_unique
ON email_ingress_address(household_id, purpose)
WHERE status IN ('PROVISIONED','ACTIVE');
```

### Meaning

`PROVISIONED`:

```text
address exists
can receive Cloudflare email
may be used for Gmail forwarding verification
must NOT create source_event / financial state
```

`ACTIVE`:

```text
forwarded bank email is the active financial provider
```

`DISABLED`:

```text
address no longer accepted for financial ingestion
```

This is not a shadow pipeline.

`PROVISIONED` exists solely to allow Deploy 1 to generate a usable email address before provider cutover.

---

# 7. Generated recipient address

The Go backend must generate the recipient.

Format:

```text
h_<32 lowercase hex chars>@richmod.link
```

Generation:

```text
16 cryptographically random bytes
→ lowercase hex
→ prefix h_
```

Use:

```go
crypto/rand
encoding/hex
```

Never derive from:

- household UUID;
- user ID;
- household name;
- user email.

Example:

```text
h_31f75ab14ed64c20b98661cbd903ea48@richmod.link
```

The local part is an opaque routing identifier.


## 7.1 Non-negotiable household routing invariant

This is the primary cross-household isolation rule.

```text
signed envelope recipient
    ↓
email_ingress_address.local_part
    ↓
exactly one household_id
    ↓
all downstream processing remains scoped to that household_id
```

The household MUST NEVER be inferred from:

```text
From
Subject
email body
bank name
sender domain
LLM output
Message-ID
R2 object key
```

Only the generated recipient address may select the household.

The recipient is part of the Cloudflare → Richmod HMAC canonical string:

```text
timestamp
+ "\n"
+ recipient
+ "\n"
+ envelopeFrom
+ "\n"
+ sha256
```

Therefore a valid request for Household A cannot be rewritten to Household B without invalidating the HMAC.

Database requirements:

```text
email_ingress_address.local_part
→ UNIQUE
→ exactly one household_id
```

After HMAC verification:

```go
address := lookupIngressAddress(recipient)

if address == nil || address.Status != "ACTIVE" {
    // Do not reveal whether the recipient exists.
    return acceptedWithoutFinancialMutation()
}

householdID := address.HouseholdID
```

For `PROVISIONED`, transport verification is allowed but financial mutation remains disabled.

Every downstream lookup/mutation MUST use the resolved `householdID`, including:

```text
bank_email_listener
email_ingress_delivery
source_event
bank_email_event
PROCESS_BANK_EMAIL payload/source event
review/proposal/ledger paths reached from that source event
```

Sender validation is always performed *inside* the already-resolved household:

```sql
SELECT ...
FROM bank_email_listener
WHERE household_id = $resolved_household_id
  AND sender_address = lower($parsed_sender)
  AND active;
```

Never perform the inverse lookup:

```text
sender → household
```

because multiple households may legitimately configure the same financial sender.

Unknown or disabled recipients MUST return a generic success response such as:

```text
202 Accepted
```

without exposing whether an address belongs to a real household.

Required isolation tests:

```text
recipient A + sender valid for A → only Household A may mutate
recipient B + same sender → only Household B may mutate
recipient A + listener that exists only in B → ignored, never routed to B
tampered recipient A→B with original HMAC → rejected
unknown recipient → 202, no household/source/job mutation
duplicate delivery → remains within original household
```

Core rule:

> Recipient selects the household. Sender only determines whether the email is allowed within that household. The LLM never selects or changes household identity.

---

# 8. `email_ingress_delivery`

Recommended:

```sql
CREATE TABLE email_ingress_delivery (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    address_id UUID NOT NULL REFERENCES email_ingress_address(id),
    household_id UUID NOT NULL REFERENCES household(id),

    listener_id UUID REFERENCES bank_email_listener(id),
    source_event_id UUID REFERENCES source_event(id),

    provider TEXT NOT NULL
        CHECK (provider IN ('CLOUDFLARE_EMAIL')),

    object_key TEXT NOT NULL,
    content_sha256 BYTEA NOT NULL,
    raw_size BIGINT,

    envelope_from TEXT,
    observed_sender TEXT,
    internet_message_id TEXT,
    subject TEXT,
    email_date TEXT,

    authentication_results TEXT,
    arc_authentication_results TEXT,

    status TEXT NOT NULL CHECK (status IN (
        'PROVISIONED_RECEIVED',
        'INGESTED',
        'IGNORED_UNMATCHED',
        'IGNORED_AUTH',
        'DUPLICATE'
    )),

    reason_code TEXT,

    received_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX email_ingress_delivery_object_unique
ON email_ingress_delivery(object_key);

CREATE UNIQUE INDEX email_ingress_delivery_payload_unique
ON email_ingress_delivery(address_id, content_sha256);
```

When an address is `PROVISIONED`:

```text
verify request
parse email
record delivery
return 202
DO NOT create source_event
```

This allows Gmail forwarding verification and transport testing before cutover without dual financial ingestion.

---

# 9. Generalize `bank_email_event`

Current field:

```text
gmail_message_id
```

Rename:

```text
message_id
```

New migration:

```sql
ALTER TABLE bank_email_event
RENAME COLUMN gmail_message_id TO message_id;
```

Update all SQL and tests.

New Cloudflare path:

```text
message_id = RFC822 Message-ID
```

Fallback if missing:

```text
sha256:<hex>
```

Do not rewrite historical values.

---

# 10. New API package

Create:

```text
apps/api/internal/emailingress/
├── auth.go
├── auth_test.go
├── mime.go
├── mime_test.go
├── service.go
├── service_test.go
├── handler.go
└── handler_test.go
```

Responsibilities must remain separated.

---

# 11. HMAC verification

`auth.go`:

```go
type SignedRequest struct {
    Timestamp    int64
    Recipient    string
    EnvelopeFrom string
    ContentHash  [32]byte
    Signature    []byte
    MessageID    string
    ObjectKey    string
}
```

Validate in this order:

```text
required headers
timestamp
recipient format
bounded body
SHA
HMAC
```

Never log:

```text
secret
full signature
raw MIME
```

---

# 12. MIME normalization

Use Go.

Prefer:

```text
net/mail
mime
mime/multipart
mime/quotedprintable
encoding/base64
```

Parse:

```text
From
Subject
Date
Message-ID
Authentication-Results
ARC-Authentication-Results
text/html
text/plain
```

Handle:

```text
multipart/alternative
multipart/mixed
base64
quoted-printable
RFC2047 headers
```

Ignore attachments for bank transaction extraction.

Prefer:

```text
HTML
```

then fallback:

```text
plain text
```

Suggested:

```go
type ParsedEmail struct {
    Sender                   string
    Subject                  string
    Date                     string
    MessageID                string
    AuthenticationResults    string
    ARCAuthenticationResults string
    HTMLBody                 string
    TextBody                 string
}
```

---

# 13. Sender/authentication boundary

Recipient determines household.

It does not prove sender identity.

Never trust only:

```text
From: configured-sender@example-bank.com
```

The current Gmail path prevents untrusted or unconfigured financial emails from reaching the LLM.

Preserve that.

Add:

```text
EMAIL_INGRESS_TRUSTED_AUTHSERV_IDS
```

Before activating the recipient, inspect real forwarded email for the configured financial senders:

```text
configured financial sender
→ primary Gmail
→ automatic forwarding
→ @richmod.link
→ Cloudflare raw .eml
```

and determine the trusted authentication-service header.

Initial policy is transport/authentication based, not institution based:

```text
trusted authserv-id
AND
acceptable authenticated-sender verdict
AND
exact active bank_email_listener sender match
```

Do not hardcode a specific bank's domain or sender in the ingress package.

The exact accepted DKIM/DMARC/ARC rule must be derived from actual forwarded-message evidence and documented in the ADR/tests.

If forwarding semantics require ARC, inspect real evidence and explicitly change the ADR/tests.

Do not guess.

No trusted authentication:

```text
no LLM
no source_event
no financial mutation
```

---

# 14. Deploy 1 — complete new flow, Gmail still present

Deploy 1 MUST be fully deployable and usable through recipient generation.

It introduces all new functionality but does not remove Gmail code.

## 14.1 Deploy 1 database

Add:

```text
email_ingress_address
email_ingress_delivery
```

Rename:

```text
bank_email_event.gmail_message_id
→
bank_email_event.message_id
```

Keep:

```text
gmail_integration
gmail_oauth_state
```

unchanged.

Keep Gmail job types.

---

# 15. Deploy 1 API configuration

Add to:

```text
apps/api/internal/config/config.go
```

```text
EmailIngressHMACSecret
EmailIngressDomain
EmailIngressTrustedAuthservIDs
```

Environment:

```text
EMAIL_INGRESS_HMAC_SECRET=<secret>
EMAIL_INGRESS_DOMAIN=richmod.link
EMAIL_INGRESS_TRUSTED_AUTHSERV_IDS=<verified authserv IDs>
```

Gmail config remains for this deploy.

Cloudflare/R2 credentials are NOT added.

---

# 16. Deploy 1 endpoint

Register:

```text
POST /finance/v1/email/inbond
```

No user session.

Use:

```text
HMAC authentication
dedicated rate limit
request ID
access log
security headers
```

Do not log body.

---

# 17. Deploy 1 household integration API

Add minimal authenticated API.

Recommended:

```http
GET  /api/v1/integrations/email-ingress
POST /api/v1/integrations/email-ingress
POST /api/v1/integrations/email-ingress/activate
POST /api/v1/integrations/email-ingress/rotate
```

## GET

Returns current address/status.

Example:

```json
{
  "address": "h_31f75ab14ed64c20b98661cbd903ea48@richmod.link",
  "status": "PROVISIONED",
  "provider": "CLOUDFLARE_EMAIL",
  "last_received_at": null
}
```

## POST create

OWNER-only.

Idempotent.

If a current PROVISIONED or ACTIVE address exists:

```text
return it
```

Otherwise generate recipient and store:

```text
status = PROVISIONED
```

This endpoint is what makes Deploy 1 fully usable.

The user can immediately copy the generated address into Gmail forwarding settings.

---

# 18. Deploy 1 PROVISIONED delivery behavior

A PROVISIONED recipient is allowed to receive email through Cloudflare.

The endpoint must:

```text
validate HMAC
validate SHA
parse MIME
persist email_ingress_delivery
update last_received_at
return 202
```

But it MUST NOT create:

```text
source_event
bank_email_event
PROCESS_BANK_EMAIL
```

This gives a transport-verification path before cutover.

Again: this is not financial shadow ingestion.

---

# 19. Deploy 1 ACTIVE delivery behavior

An ACTIVE recipient is transport-generic.

For every incoming message:

```text
recipient
→ household
→ parsed sender
→ exact active bank_email_listener for that household
→ trusted authentication verdict
→ generic bank email pipeline
```

No provider name, sender address, subject pattern, or bank domain may be hardcoded in `emailingress`.

An ACTIVE recipient with valid sender/auth must run one DB transaction:

```text
email_ingress_delivery
    ↓
source_event
    ↓
source_event_payload metadata
    ↓
bank_email_event
    ↓
PROCESS_BANK_EMAIL
```

Suggested source event:

```text
source_type       = BANK_EMAIL
external_id       = rfc822:<Message-ID>
                    OR sha256:<hash>
raw_payload_ref   = r2://richmod-email-raw/<object-key>
payload_hash      = raw MIME SHA-256
processing_status = RECEIVED
parser_name       = cloudflare-email-ingress
parser_version    = 1
```

`source_event_payload` stores metadata only.

Raw MIME remains in R2.

Reuse:

```text
PROCESS_BANK_EMAIL
```

Do not add:

```text
PROCESS_CLOUDFLARE_EMAIL
```

---

# 20. Deploy 1 provider cutover

`POST /api/v1/integrations/email-ingress/activate`

OWNER-only.

This is the key atomic cutover.

Within one PostgreSQL transaction:

```text
lock email_ingress_address
        ↓
require status = PROVISIONED
        ↓
set address = ACTIVE
        ↓
if gmail_integration exists:
    set status = DISCONNECTED
        ↓
audit
        ↓
commit
```

After commit:

```text
Cloudflare = financial provider
Gmail API  = disabled for household
```

There must never be a committed state in which both are active.

---

# 21. Deploy 1 Gmail behavior after cutover

Because Gmail code still exists until Deploy 2, it must safely respect:

```text
gmail_integration.status = DISCONNECTED
```

Modify Gmail logic minimally.

## Watch renewal

Do not seed renewal for:

```text
DISCONNECTED
```

## Gmail Pub/Sub/history

If notification/job belongs to a DISCONNECTED integration:

```text
return terminal success
do not ingest message
```

Any old queued:

```text
PROCESS_GMAIL_HISTORY
```

must no-op for that household.

This prevents a late Gmail event from racing the now-active Cloudflare path.

Do not redesign Gmail beyond what is necessary for safe temporary coexistence.

---

# 22. Deploy 1 generated-address rollout sequence

After Deploy 1 production deployment:

```text
1. deploy API + worker + migration

2. verify:
   https://api.investdx.biz.id/healthz
   https://api.investdx.biz.id/readyz

3. call:
   POST /api/v1/integrations/email-ingress

4. receive:
   h_<32hex>@richmod.link
   status = PROVISIONED

5. add generated address to primary Gmail forwarding settings

6. complete Gmail forwarding verification

7. create Gmail filter for intended configured financial emails only

8. send/receive a forwarded email

9. confirm:
   Email Routing
   → ingress Worker
   → R2
   → Queue
   → delivery Worker
   → POST /finance/v1/email/inbond
   → email_ingress_delivery

10. inspect actual raw MIME authentication headers

11. configure/finalize trusted authserv IDs if necessary

12. activate:
    POST /api/v1/integrations/email-ingress/activate

13. confirm:
    email_ingress_address = ACTIVE
    gmail_integration = DISCONNECTED

14. receive a real financial transaction notification

15. confirm:
    source_event
    → bank_email_event
    → PROCESS_BANK_EMAIL
    → existing review/transaction flow
```

At this point Deploy 1 is successful.

---

# 23. Deploy 1 acceptance criteria

All must pass before Deploy 2:

- generated recipient address works;
- generated address is household-scoped;
- Gmail forwarding verification can reach it;
- R2 receives raw MIME;
- Queue delivers to Go API;
- endpoint `/finance/v1/email/inbond` accepts valid HMAC request;
- PROVISIONED recipient creates no financial source event;
- activation is atomic;
- Gmail becomes DISCONNECTED;
- ACTIVE Cloudflare email creates exactly one source event;
- existing `PROCESS_BANK_EMAIL` succeeds;
- duplicate Cloudflare delivery creates no duplicate transaction;
- late Gmail history job after cutover creates no new bank event.

Only then proceed to Deploy 2.

---

# 24. Deploy 2 — Gmail sunset and code deletion

Deploy 2 assumes all production household bank-email ingestion has been migrated to ACTIVE Cloudflare recipient addresses.

Deploy 2 permanently removes Gmail as an application integration.

---

# 25. Deploy 2 API removal

Delete:

```text
apps/api/internal/gmail/oauth.go
apps/api/internal/gmail/oauth_test.go
apps/api/internal/gmail/pubsub.go
apps/api/internal/gmail/pubsub_test.go
```

Remove Gmail import/initialization from:

```text
apps/api/cmd/api/main.go
```

Remove routes:

```text
GET  /api/v1/integrations/gmail/connect
GET  /api/v1/integrations/gmail/callback
POST /webhooks/gmail/pubsub
```

Keep:

```text
POST /finance/v1/email/inbond
```

---

# 26. Deploy 2 worker removal

Delete:

```text
apps/worker/internal/gmail/client.go
apps/worker/internal/gmail/client_test.go
apps/worker/internal/gmail/processor.go
apps/worker/internal/gmail/processor_test.go
```

Remove from:

```text
apps/worker/cmd/worker/main.go
```

all:

```text
workerGmail imports
Gmail processor initialization
Gmail maintenance
SeedRenewalJobs
PROCESS_GMAIL_HISTORY
RENEW_GMAIL_WATCH
Gmail job payload decoding
```

Keep:

```text
PROCESS_BANK_EMAIL
```

unchanged.

---

# 27. Deploy 2 config removal

Remove:

```text
GMAIL_OAUTH_CLIENT_PATH
GMAIL_MAILBOX
GMAIL_TOKEN_ENCRYPTION_KEY
GMAIL_PUBSUB_AUDIENCE
GMAIL_PUBSUB_SERVICE_ACCOUNT
GMAIL_PUBSUB_TOPIC
```

from:

```text
apps/api/internal/config/config.go
.env.example
compose.yaml
compose.production.yaml
deployment docs
Sumopod environment
```

Retain:

```text
EMAIL_INGRESS_HMAC_SECRET
EMAIL_INGRESS_DOMAIN
EMAIL_INGRESS_TRUSTED_AUTHSERV_IDS
```

---

# 28. Deploy 2 database cleanup

Use a new migration.

Do not modify historical migrations.

Before dropping Gmail tables, remove/terminalize obsolete Gmail jobs:

```text
PROCESS_GMAIL_HISTORY
RENEW_GMAIL_WATCH
```

Inspect current `job` schema and use the repository's appropriate terminal/delete convention.

Then drop Gmail-only application state:

```sql
DROP TABLE IF EXISTS gmail_oauth_state;
DROP TABLE IF EXISTS gmail_integration;
```

Do not delete historical financial evidence.

Keep:

```text
source_event
source_event_payload
bank_email_event
bank_email_extraction
transaction_proposal
transaction
transaction_evidence
audit
```

Historical source events produced by Gmail remain valid records.

---

# 29. Deploy 2 dependency cleanup

Run:

```bash
go mod tidy
```

in affected Go modules.

Remove Gmail/Google libraries if no longer used anywhere else.

Do not remove dependencies used by unrelated Google integrations without verifying references.

---

# 30. Deploy 2 Google infrastructure cleanup

After the application deployment succeeds:

- revoke Richmod Gmail OAuth authorization;
- remove obsolete OAuth client JSON from Sumopod;
- delete Google Pub/Sub topic/subscription used only for Gmail push;
- remove Gmail push service-account configuration if unused;
- remove Gmail API-specific Google Cloud configuration.

Do not remove the user's Gmail account.

Do not disable Gmail forwarding.

Gmail remains:

```text
mail client + forwarding source
```

---

# 31. Code references Codex MUST inspect

## Repository rules

```text
AGENTS.md
```

Mandatory principles:

```text
Go backend
PostgreSQL canonical state
Go owns financial mutations
LLM is untrusted
evidence preserved
idempotent webhooks/jobs
generic bank email pipeline
architecture changes require ADR
worktree workflow required
```

## Product docs

```text
docs/RICHMOD_PRODUCT_ALIGNMENT_V2.md
docs/FAMILY_FINANCE_SYSTEM_ANALYSIS_CODEX.md
docs/MVP_COMPLETION_CHECKLIST.md
```

Update current docs to replace Gmail as the active long-term ingestion architecture.

## Source-event schema

```text
db/migrations/00002_core_ledger.sql
```

Reuse:

```text
external_id
raw_payload_ref
payload_hash
processing_status
parser_name
parser_version
```

and existing dedupe indexes.

## Gmail migration

```text
db/migrations/00005_gmail_oauth.sql
```

Do not edit it.

Deploy 2 drops its runtime tables through a new migration.

## API boot

```text
apps/api/cmd/api/main.go
```

Deploy 1:

```text
add email ingress
keep Gmail
```

Deploy 2:

```text
remove Gmail
keep email ingress
```

## API config

```text
apps/api/internal/config/config.go
```

Deploy 1:

```text
add EMAIL_INGRESS_*
keep GMAIL_*
```

Deploy 2:

```text
remove GMAIL_*
```

## HTTP middleware

```text
apps/api/internal/platform/httpmw/middleware.go
```

Ensure `/finance/v1/email/inbond` works through existing middleware without browser session/auth.

## Gmail API

```text
apps/api/internal/gmail/oauth.go
apps/api/internal/gmail/pubsub.go
```

Temporary during Deploy 1.

Deleted in Deploy 2.

## Gmail worker

```text
apps/worker/internal/gmail/client.go
apps/worker/internal/gmail/processor.go
```

Deploy 1 only needs minimal `DISCONNECTED` safety.

Deleted in Deploy 2.

## Worker boot

```text
apps/worker/cmd/worker/main.go
```

Existing generic bank job remains:

```text
PROCESS_BANK_EMAIL
```

## Bank email pipeline

```text
apps/worker/internal/bankemail/event.go
apps/worker/internal/bankemail/processor.go
apps/worker/internal/bankemail/policy.go
apps/worker/internal/bankemail/validator.go
```

Do not add Cloudflare provider branches here.

Ingress differences stop before `bank_email_event`.

---

# 32. Required tests — Deploy 1

## Recipient generation

```text
16 random bytes
→ h_<32 lowercase hex>
```

Test:

- format;
- uniqueness;
- idempotent create;
- household isolation;
- owner authorization.

## HMAC

```text
valid vector
wrong secret
changed recipient
changed envelope sender
changed SHA
stale timestamp
future timestamp
invalid hex
```

## Content SHA

```text
match → accepted
mismatch → 400
```

## PROVISIONED state

Valid email to PROVISIONED address:

```text
email_ingress_delivery created
source_event NOT created
bank_email_event NOT created
PROCESS_BANK_EMAIL NOT created
```

## ACTIVE state

Valid trusted bank email:

```text
1 source_event
1 bank_email_event
1 PROCESS_BANK_EMAIL
```

## Unknown/disabled

```text
202
no financial state
```

## Authentication boundary

```text
trusted auth + active listener → eligible
wrong sender → ignored
fake/untrusted Authentication-Results → ignored
dkim failure → ignored
dmarc failure → ignored
inactive listener → ignored
```

## Duplicate

Same raw email twice:

```text
second response 2xx
source count unchanged
job count unchanged
transaction count unchanged
```

## Cutover

Before:

```text
address = PROVISIONED
gmail = WATCH_ACTIVE
```

After activation transaction:

```text
address = ACTIVE
gmail = DISCONNECTED
```

No intermediate dual-active state.

## Late Gmail job

After cutover:

```text
PROCESS_GMAIL_HISTORY
→ no bank ingestion
→ terminal success
```

---

# 33. Required tests — Deploy 2

After deletion:

```bash
grep -R "PROCESS_GMAIL_HISTORY" -n apps || true
grep -R "RENEW_GMAIL_WATCH" -n apps || true
grep -R "/integrations/gmail" -n apps || true
grep -R "/webhooks/gmail" -n apps || true
grep -R "GMAIL_" -n apps compose*.yaml .env.example || true
grep -R "internal/gmail" -n apps || true
```

Expected active runtime references:

```text
none
```

Historical docs/migrations may mention Gmail where appropriate.

Run:

```bash
go test ./...
```

in affected modules.

Run:

```bash
git diff --check
```

---

# 34. Production deployment sequence

## DEPLOY 1

```text
A. deploy DB migration
B. deploy API containing new ingress
C. deploy worker with temporary Gmail coexistence safeguards
D. set EMAIL_INGRESS_* env
E. point Cloudflare delivery Worker to:
   https://api.investdx.biz.id/finance/v1/email/inbond
F. enable Queue consumer
G. generate recipient from Richmod API
H. configure Gmail forwarding
I. verify PROVISIONED transport
J. activate recipient / disconnect Gmail
K. verify real ACTIVE bank transaction end-to-end
```

Do not perform Deploy 2 until step K succeeds.

## DEPLOY 2

```text
A. remove Gmail code
B. remove Gmail jobs
C. remove Gmail env
D. migrate/drop Gmail-only tables
E. deploy API
F. deploy worker
G. run regression tests / health checks
H. revoke external Google OAuth/PubSub resources
```

Exactly two application deployment phases.

---

# 35. Final environment

After Deploy 2, API env includes:

```text
EMAIL_INGRESS_HMAC_SECRET
EMAIL_INGRESS_DOMAIN=richmod.link
EMAIL_INGRESS_TRUSTED_AUTHSERV_IDS
```

No:

```text
GMAIL_*
```

Cloudflare delivery worker:

```text
RICHMOD_INGRESS_URL=https://api.investdx.biz.id/finance/v1/email/inbond
RICHMOD_INGRESS_SECRET=<matching secret>
```

---

# 36. Acceptance criteria

## Deploy 1 complete

- backend generates real `h_<token>@richmod.link`;
- address can be used in Gmail forwarding;
- Cloudflare delivery reaches `/finance/v1/email/inbond`;
- R2 raw evidence is retained;
- PROVISIONED mode creates no financial state;
- activation atomically makes Cloudflare ACTIVE and Gmail DISCONNECTED;
- real financial email enters existing `PROCESS_BANK_EMAIL`;
- duplicate retries are safe;
- old Gmail job cannot mutate after cutover.

## Deploy 2 complete

- Gmail API code deleted;
- Gmail worker deleted;
- Gmail routes deleted;
- Gmail jobs deleted;
- Gmail config/env deleted;
- Gmail DB runtime tables deleted;
- unused Google dependencies removed;
- external OAuth/PubSub infrastructure removed;
- Cloudflare is the only application bank-email ingress;
- Gmail is only the forwarding source.

---

# 37. Explicit Codex constraints

Do not:

```text
create financial shadow ingestion
run Gmail and Cloudflare as dual-active
rewrite the bankemail processor
add provider-specific Cloudflare parser
create PROCESS_CLOUDFLARE_EMAIL
invoke LLM from HTTP ingress
create ledger rows directly in ingress
trust From alone
trust arbitrary Authentication-Results
store HMAC secret in DB
add R2 API credentials to Go
perform Gmail code deletion in Deploy 1
```

Do:

```text
generate recipient in Deploy 1
use PROVISIONED state before activation
perform atomic provider cutover
reuse source_event
reuse bank_email_event
reuse bank_email_listener
reuse PROCESS_BANK_EMAIL
delete Gmail completely in Deploy 2
preserve historical financial evidence
```

---

# 38. Codex completion reports

## Deploy 1 report

```text
Branch:
Worktree:
Base SHA:
Implementation commit:
Merge commit:
Pushed main SHA:

ADR:
Migration:
Generated-address API:
Ingress endpoint:
https://api.investdx.biz.id/finance/v1/email/inbond

Tests:
Deployment:
EMAIL_INGRESS env configured:
Cloudflare consumer enabled:
Generated recipient:
Gmail forwarding verified:
Recipient status:
Gmail integration status:
Configured financial sender end-to-end test:
Risks / remaining steps:
```

Do not print secrets.

## Deploy 2 report

```text
Branch:
Worktree:
Base SHA:
Implementation commit:
Merge commit:
Pushed main SHA:

Gmail files deleted:
Gmail routes removed:
Gmail jobs removed:
Gmail env removed:
Gmail tables dropped:
Dependencies tidied:
Tests:

API deployed:
Worker deployed:
Health:
Cloudflare ingest still verified:

Google OAuth revoked:
Pub/Sub removed:
OAuth material removed:

Remaining issues:
```

Do not claim operational cleanup was completed unless it actually was.
