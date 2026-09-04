# Richmod Super Admin — Platform Operations Console

**Repository:** `raufimusaddiq/richmod`  
**Baseline reviewed:** `main` @ `11fc2c0f5e788a2383f0a3d34da774d94acf71e2`  
**Baseline commit:** `merge: record analytics composition delivery`  
**Review date:** 2026-08-30  
**Target:** latest `main` at implementation time  
**Scope:** turn the existing `/admin` page into a Super Admin platform monitoring and administration console  
**Stack:** Go + Next.js + React + JavaScript + PostgreSQL + existing Recharts

> This is a platform-operations console for Richmod Super Admins.
>
> It is not a household finance feature and it must not weaken household isolation,
> financial-state rules, or privacy boundaries.
>
> Prefer existing PostgreSQL operational data over introducing new infrastructure.
> Do not add Grafana, Loki, Elasticsearch, Redis, Kafka, or another observability
> service in this task.

---

# 0. Codex startup instructions

Before editing:

1. checkout/pull the latest `main`;
2. read root `AGENTS.md`;
3. verify whether `main` moved beyond the baseline SHA above;
4. inspect the current `/admin` page and backend admin handler;
5. inspect operations status, jobs, retry logs, LLM calls, worker heartbeat, audit tables, and current migrations;
6. use a dedicated branch + linked worktree;
7. update relevant docs in the same branch if behavior changes;
8. do not deploy unless explicitly requested.

Read at minimum:

```text
AGENTS.md

apps/web/app/admin/page.js
apps/web/app/components/AppShell.js
apps/web/app/globals.css

apps/api/internal/admin/handler.go
apps/api/internal/operations/handler.go
apps/api/cmd/api/main.go

db/migrations/00002_core_ledger.sql
db/migrations/00003_telegram_intake.sql
db/migrations/00012_operations.sql
db/migrations/00016_telegram_interactive_lane.sql
db/migrations/00028_job_lane_lifecycle.sql
db/migrations/00029_review_llm_cycle_contracts.sql
db/migrations/00032_job_retry_log.sql
```

Also inspect any newer migrations before choosing a migration number.

Suggested branch/worktree:

```bash
git checkout main
git pull --ff-only

git worktree add \
  -b feat/super-admin-platform-console \
  ../family-finance-worktrees/super-admin-platform-console \
  main
```

---

# 1. Current repository state

The existing `/admin` page is intentionally minimal.

Current web behavior:

```text
GET /api/v1/auth/me
GET /api/v1/admin/users
```

and it renders only a basic user list.

Current admin backend already supports:

```text
GET   /api/v1/admin/users
PATCH /api/v1/admin/users/{id}

GET   /api/v1/admin/households
GET   /api/v1/admin/households/{householdId}/members
POST  /api/v1/admin/households/{householdId}/members
```

All routes pass through the existing `admin.Require` Super Admin authorization guard.

Current operational data already exists in PostgreSQL:

```text
job
job_retry_log
worker_heartbeat
llm_call
source_event
review_item / review_request
audit_log
household
household_member
user
gmail_integration
telegram_identity
```

Important current capabilities:

- `worker_heartbeat` tracks worker start/last-seen state;
- `job` tracks lane, lifecycle, attempts, run timing, and terminal state;
- `job_retry_log` persistently records retry error class and timing without raw payload/error content;
- `llm_call` records task, protocol, model, success/failure, error class, duration, token counts, optional cost, and attempt;
- `audit_log` is household-scoped financial/domain audit;
- `/api/v1/operations/status` already calculates useful queue lane p50/p95 metrics, but it is an OWNER household endpoint and must not be reused as the global Super Admin API.

The repository migration head already includes at least:

```text
00032_job_retry_log.sql
```

Use the next available migration number after re-reading latest `main`.

---

# 2. Product goal

Replace the current single-purpose Admin page with:

```text
SUPER ADMIN
Platform Operations Console
```

The console answers:

```text
Is Richmod healthy?

Are workers alive?

Is a queue lane backing up?

What jobs are failing?

Is the LLM gateway healthy?

How much LLM usage/cost do we have?

Which households/users are affected?

What structured operational failures happened recently?

What administrative actions were performed?
```

The console should be operationally useful without exposing raw private financial data by default.

---

# 3. Final information architecture

Use one `/admin` route with internal tabs.

Preferred tabs:

```text
Overview
Jobs
LLM
Logs
Households
Users
Audit
```

Desktop:

```text
ADMINISTRASI PLATFORM
Platform Operations Console

[Overview] [Jobs] [LLM] [Logs] [Households] [Users] [Audit]

<active tab content>
```

Do not add seven new sidebar navigation entries.

The existing Super Admin-only `Admin` sidebar link remains the single entry point.

On mobile, the internal admin tabs should horizontally scroll or collapse cleanly.

---

# 4. Global design language

The Super Admin console should visually belong to Richmod but be denser than household-facing screens.

## 4.1 Base visual style

Reuse current Richmod:

```text
page background
white `.surface` cards
existing border color
existing 15–17px radius
subtle shadow
existing Inter/system typography
green primary action
muted grey supporting text
```

Do not create a completely separate black/devops theme.

The console should look like:

```text
Richmod
+
professional operations dashboard
```

not:

```text
Grafana clone
terminal emulator
cyberpunk admin UI
```

## 4.2 Color semantics

Use color only for operational state:

```text
Healthy / Success
→ existing Richmod green

Running / informational
→ muted blue

Pending / warning / degraded
→ amber

Failed / unhealthy
→ muted red/coral

Neutral
→ grey
```

Avoid assigning unique colors to every job type.

## 4.3 Density

Household UI is relatively spacious.

Admin tables may be denser:

```text
table row height: ~42–48px
card padding: ~18–20px
small label: 10–11px
table text: 11–12px
primary metric: 20–26px
```

Do not shrink text below readable size.

## 4.4 IDs and technical fields

Use monospace styling only for:

```text
job IDs
request IDs
error classes
protocol
model
worker IDs
```

Do not render the entire console in monospace.

Long UUIDs should be visually truncated but copyable.

Example:

```text
6af8…9c31
```

with title/copy action for the complete ID.

## 4.5 Refresh behavior

Overview may refresh automatically every:

```text
30 seconds
```

with a visible:

```text
Terakhir diperbarui 10:14:22
```

and a manual:

```text
Perbarui
```

button.

Do not poll heavy tables every few seconds.

Jobs/Logs/LLM lists refresh only:

```text
on explicit refresh
on filter change
on pagination
```

unless a small bounded auto-refresh is already simple and low-cost.

---

# 5. Shared admin page shell

Keep:

```jsx
<AppShell
  user={me}
  eyebrow="ADMINISTRASI PLATFORM"
  title="Platform Operations Console"
>
```

Under the page title add an internal admin navigation bar.

Suggested component:

```text
apps/web/app/admin/components/AdminTabs.js
```

or equivalent.

Example:

```jsx
const tabs = [
  ["overview", "Overview"],
  ["jobs", "Jobs"],
  ["llm", "LLM"],
  ["logs", "Logs"],
  ["households", "Households"],
  ["users", "Users"],
  ["audit", "Audit"],
];
```

Prefer URL query state:

```text
/admin?tab=overview
/admin?tab=jobs
/admin?tab=llm
```

rather than purely ephemeral React state.

Benefits:

```text
refresh preserves tab
links can be shared
browser back/forward works
```

Do not create a client-side routing framework.

---

# 6. Authorization boundary

Every new admin API must be protected by:

```text
authHandler.RequireSession(...)
adminHandler.Require(...)
```

Frontend hiding is not authorization.

A non-superadmin calling any:

```text
/api/v1/admin/*
```

monitoring endpoint must receive:

```http
403
```

Do not allow household OWNER role to access global platform operations.

Do not reuse `/api/v1/operations/status` for global monitoring because that endpoint has household OWNER semantics.

---

# 7. P0 — Overview tab

## 7.1 Purpose

Answer:

> "Is the Richmod platform healthy right now?"

## 7.2 Desktop target

```text
Overview

┌─────────────┬─────────────┬─────────────┬─────────────┐
│ PLATFORM    │ WORKER      │ QUEUE       │ FAILED 24H  │
│ Healthy     │ Healthy     │ 3 pending   │ 1           │
│ checked now │ seen 4s ago │ 1 running   │ jobs        │
└─────────────┴─────────────┴─────────────┴─────────────┘

┌─────────────┬─────────────┬─────────────┬─────────────┐
│ LLM 24H     │ LLM SUCCESS │ OPEN REVIEW │ HOUSEHOLDS  │
│ 73 calls    │ 98.6%       │ 4           │ 2 active    │
└─────────────┴─────────────┴─────────────┴─────────────┘

┌───────────────────────────────┬─────────────────────────────┐
│ JOB QUEUE BY LANE             │ LLM 24H                     │
│                               │                             │
│ INTERACTIVE                   │ Calls             73        │
│ 0 pending · 0 running         │ Failed             1        │
│ p95 180ms                     │ p95             4.2s        │
│                               │ Tokens          182k        │
│ DEFAULT                       │ Cost           $0.42        │
│ ...                           │                             │
│                               │ [Lihat LLM →]              │
│ BACKGROUND                    │                             │
│ ...                           │                             │
└───────────────────────────────┴─────────────────────────────┘

┌─────────────────────────────────────────────────────────────┐
│ RECENT OPERATIONAL EVENTS                                   │
│ 10:04 JOB   PROCESS_BANK_EMAIL retry · TIMEOUT              │
│ 10:03 LLM   BANK_EMAIL failed · PROVIDER_5XX                │
│ 09:52 SOURCE BANK_EMAIL processing failed                   │
│                                             [Lihat Logs →]  │
└─────────────────────────────────────────────────────────────┘
```

## 7.3 Overview health rules

Define deterministic health.

Suggested platform status:

```text
HEALTHY
worker heartbeat healthy
AND no overdue INTERACTIVE backlog
AND no severe current queue condition

DEGRADED
worker heartbeat stale
OR recent failures > 0
OR due queue age exceeds conservative threshold

UNHEALTHY
worker heartbeat absent/stale for a clearly unhealthy period
OR database query fails
```

Do not make a single historical failed job permanently mark the platform unhealthy.

Suggested worker heartbeat:

```text
healthy if newest last_seen_at < 60 seconds old
```

Match existing operations behavior unless current worker heartbeat cadence suggests a better threshold.

Queue warnings should be lane-sensitive.

Example:

```text
INTERACTIVE oldest due > 5s
→ warning

DEFAULT oldest due > 30s
→ warning

BACKGROUND oldest due > 2m
→ warning
```

Before hardcoding thresholds, inspect actual worker cadence and expected workload.

Keep thresholds in Go constants, not scattered in JSX.

## 7.4 Overview API

Add:

```http
GET /api/v1/admin/overview
```

Response shape:

```json
{
  "status": "HEALTHY",
  "checkedAt": "2026-08-30T10:10:00+07:00",
  "worker": {
    "healthy": true,
    "activeWorkers": 1,
    "lastHeartbeatAt": "2026-08-30T10:09:57+07:00"
  },
  "jobs": {
    "pending": 3,
    "running": 1,
    "failed24h": 1,
    "lanes": [
      {
        "lane": "INTERACTIVE",
        "pending": 0,
        "running": 0,
        "oldestDueAgeMs": null,
        "executionP50Ms": 120,
        "executionP95Ms": 180
      }
    ]
  },
  "llm": {
    "calls24h": 73,
    "failed24h": 1,
    "successRate": 0.9863,
    "p95DurationMs": 4200,
    "inputTokens": 145000,
    "outputTokens": 37000,
    "cost": "0.42000000"
  },
  "reviews": {
    "open": 4
  },
  "households": {
    "total": 2,
    "active": 2
  },
  "users": {
    "total": 3,
    "active": 3
  },
  "integrations": {
    "gmailConnected": 2,
    "telegramLinked": 3,
    "llmGatewayConfigured": true,
    "llmProtocol": "responses"
  }
}
```

If `cost` is unavailable/null across calls:

```json
"cost": null
```

Do not fake a cost estimate.

---

# 8. P0 — Jobs tab

## 8.1 Purpose

Answer:

```text
What work is queued?
What is running?
What failed?
Why did it retry?
Is one lane backing up?
```

## 8.2 Filters

```text
Status
Lane
Job Type
Time Range
Search Job ID
```

Default:

```text
24h
all statuses
newest first
```

## 8.3 Jobs table

Desktop columns:

```text
Status
Job Type
Lane
Attempts
Duration
Updated
Job ID
```

Do not display `payload_json` in the table.

Do not display raw `last_error` in the table.

## 8.4 Job detail drawer

Click row → right-side drawer.

Desktop width:

```text
520–620px
```

Sections:

```text
Job
- ID
- type
- lane
- status

Lifecycle
- created
- run after
- started
- finished
- locked by

Attempts
- current / max

Retry history
- attempt
- error class
- duration
- failed at
- retried at
```

Optional references only if allowlisted:

```text
source_event_id
document_id
insight_id
review_item_id
```

Never show arbitrary `payload_json`.

## 8.5 Jobs APIs

Add:

```http
GET /api/v1/admin/jobs
GET /api/v1/admin/jobs/{id}
```

List query:

```text
status
lane
type
from
to
q
limit
cursor
```

Use keyset pagination.

Default:

```text
limit = 50
max limit = 100
```

No dangerous mutation buttons in V1:

```text
Retry now
Cancel
Delete
Force success
Edit attempts
```

---

# 9. P0 — LLM tab

## 9.1 Purpose

Monitor Cloud LLM Gateway usage and failures.

## 9.2 KPI row

```text
Calls 24h
Success Rate
P95 Latency
Tokens 24h
Cost 24h
```

If cost is null:

```text
—
```

not `$0.00`.

## 9.3 Layout

```text
┌─────────────────────────────────────┬─────────────────────┐
│ CALLS BY HOUR                       │ STATUS              │
│ small 24h bar chart                 │ Success / Failed    │
└─────────────────────────────────────┴─────────────────────┘

TASK BREAKDOWN
Task | Calls | Failed | P50 | P95 | Tokens | Cost

RECENT CALLS
time | task | protocol | model | status | latency | tokens
```

Use existing Recharts.

One useful chart is enough.

## 9.4 Privacy

Never show:

```text
prompt text
email body
document contents
Telegram message
raw model output
tool arguments
raw provider response
API key
gateway headers
```

Only metadata from `llm_call`.

## 9.5 APIs

```http
GET /api/v1/admin/llm/summary?range=24h
GET /api/v1/admin/llm/calls?range=24h&task=&status=&limit=&cursor=
```

Include:

```text
calls
success/fail
p50/p95
input/output tokens
cost
protocol
model
task
created_at
```

---

# 10. P0 — Logs tab

For this iteration:

```text
Logs = Structured Operational Events
```

Not raw stdout/container logs.

Do not persist every `slog` line to PostgreSQL.

Build the timeline from safe persistent sources:

```text
job_retry_log
failed llm_call
FAILED source_event
terminal failed jobs
```

Do not invent worker incident history that is not persisted.

Filters:

```text
Event Type
Severity
Component
Time Range
Reference ID
```

Normalized types:

```text
JOB_RETRY
JOB_FAILED
LLM_FAILED
SOURCE_FAILED
```

Example:

```text
10:04:22 WARN  JOB_RETRY     PROCESS_BANK_EMAIL  TIMEOUT
10:03:58 ERROR LLM_FAILED    BANK_EMAIL          PROVIDER_5XX
09:52:08 ERROR SOURCE_FAILED BANK_EMAIL          —
```

API:

```http
GET /api/v1/admin/logs
```

Query:

```text
type
severity
component
from
to
q
limit
cursor
```

Perform bounded SQL aggregation/UNION server-side.

Do not fetch all source tables and merge in JavaScript.

---

# 11. P1 — Households tab

Global list columns:

```text
Household
Members
Transactions
Open Reviews
Integrations
Last Activity
Created
```

Do not show finance amounts/merchant/transaction descriptions by default.

Search:

```text
household name
household ID
member email
```

Household detail drawer:

```text
Overview
Members
Integrations
Operations
Recent Audit
```

Overview:

```text
ID
name
timezone
created
member count
transaction count
open review count
last source activity
```

Integrations:

```text
Gmail connected/disconnected
active bank listener count
Telegram linked identity count
primary salary source configured yes/no
```

Never show OAuth/secrets.

Operations:

```text
recent attributable jobs
recent LLM calls
failed source events
open reviews
```

Prefer:

```http
GET /api/v1/admin/households/{id}/overview
```

to frontend N+1 requests.

---

# 12. P1 — Users tab

Move existing user management here.

Columns:

```text
User
Email
Status
Households
Password
Role
Created
```

Filters:

```text
name/email
active/inactive
superadmin/non-superadmin
```

Actions:

```text
activate/deactivate
grant/revoke Super Admin
```

Hardening requirements:

```text
explicit confirmation for privilege changes
do not accidentally disable current admin
do not allow removal/deactivation of final active Super Admin
transactional update
platform audit row
```

Do not use a tiny checkbox for destructive privilege changes.

---

# 13. P1 — Audit tab

Do not make existing `audit_log.household_id` nullable.

Existing:

```text
audit_log
```

remains household/domain audit.

Add separate platform audit:

```sql
CREATE TABLE platform_audit_log (
    id BIGSERIAL PRIMARY KEY,
    actor_user_id UUID NOT NULL REFERENCES "user"(id),
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    metadata_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    request_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX platform_audit_log_created_idx
ON platform_audit_log(created_at DESC);

CREATE INDEX platform_audit_log_actor_time_idx
ON platform_audit_log(actor_user_id, created_at DESC);

CREATE INDEX platform_audit_log_entity_idx
ON platform_audit_log(entity_type, entity_id, created_at DESC);
```

Use the next migration number from latest `main`.

At minimum audit:

```text
ADMIN_USER_ACTIVATE
ADMIN_USER_DEACTIVATE
ADMIN_GRANT_SUPERADMIN
ADMIN_REVOKE_SUPERADMIN
ADMIN_ADD_HOUSEHOLD_MEMBER
```

Metadata may include:

```text
target email
old/new active state
old/new superadmin state
household ID
role
```

Never include:

```text
password hash
session token
OAuth token
raw job payload
LLM prompt/output
bank/document content
```

Audit UI sub-tabs:

```text
Platform
Household
```

---

# 14. System/integrations on Overview

Show:

```text
Worker
LLM Gateway
Gmail
Telegram
```

Example:

```text
Worker
Healthy
1 active · seen 4s ago

LLM Gateway
Configured
responses

Gmail
2 connected households

Telegram
3 linked identities
```

Never expose secret/config values.

---

# 15. Backend package structure

Do not let `admin/handler.go` grow into a giant file.

Suggested:

```text
apps/api/internal/admin/
  handler.go
  overview.go
  jobs.go
  llm.go
  logs.go
  households.go
  users.go
  audit.go
```

Keep handlers thin.

Do not add arbitrary SQL endpoints.

Never create:

```http
POST /api/v1/admin/query
```

---

# 16. Suggested routes

```http
GET /api/v1/admin/overview

GET /api/v1/admin/jobs
GET /api/v1/admin/jobs/{id}

GET /api/v1/admin/llm/summary
GET /api/v1/admin/llm/calls

GET /api/v1/admin/logs

GET /api/v1/admin/households
GET /api/v1/admin/households/{householdId}/overview
GET /api/v1/admin/households/{householdId}/members

GET /api/v1/admin/users
PATCH /api/v1/admin/users/{id}

GET /api/v1/admin/audit/platform
GET /api/v1/admin/audit/household
```

All routes:

```go
authHandler.RequireSession(
    adminHandler.Require(...)
)
```

---

# 17. Pagination

Paginate growing lists:

```text
jobs
llm calls
logs
platform audit
household audit
users/households if needed
```

Prefer keyset pagination using:

```text
(created_at, stable id)
```

Frontend sees opaque:

```json
{
  "items": [],
  "nextCursor": "..."
}
```

Do not use large OFFSET pagination.

---

# 18. Query performance

Inspect EXPLAIN before adding indexes.

Likely queries:

```text
job by status/lane/time
llm_call by created_at/task/status
job_retry_log by failed_at/job_type
platform audit by created_at
household audit globally by created_at
source_event failed by time
```

Add narrow indexes only if needed.

Do not duplicate existing indexes.

---

# 19. Data minimization

Never return:

```text
password_hash
session token_hash
OAuth tokens
LLM API keys
DB URL
source_event_payload.payload_json
raw bank email body
document bytes
raw Telegram text
raw LLM prompt
raw LLM output
job.payload_json
```

Job references must be allowlisted fields only.

Prefer `error_class` over arbitrary error strings.

---

# 20. Frontend structure

Avoid a 1000-line admin page.

Suggested:

```text
apps/web/app/admin/page.js

apps/web/app/admin/components/
  AdminTabs.js
  AdminOverview.js
  AdminJobs.js
  AdminJobDrawer.js
  AdminLLM.js
  AdminLogs.js
  AdminHouseholds.js
  AdminHouseholdDrawer.js
  AdminUsers.js
  AdminAudit.js
  AdminStatusBadge.js
  AdminTable.js
```

JavaScript only.

---

# 21. Detailed style specification

## Tabs

```css
.admin-tabs {
  display: flex;
  gap: 6px;
  padding: 5px;
  margin-bottom: 14px;
  overflow-x: auto;
  border: 1px solid #dfe5df;
  border-radius: 13px;
  background: #eef2ed;
}

.admin-tabs a {
  flex: 0 0 auto;
  padding: 9px 13px;
  border-radius: 9px;
  color: #667169;
  font-size: 12px;
  font-weight: 750;
  text-decoration: none;
}

.admin-tabs a.active {
  color: #245f40;
  background: #fff;
  box-shadow: 0 1px 5px #2443310d;
}
```

Reuse existing tokens if equivalent.

## Summary cards

```css
.admin-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.admin-metrics article {
  padding: 17px 18px;
}

.admin-metrics span {
  color: #6f7971;
  font-size: 10px;
  font-weight: 800;
  letter-spacing: .08em;
}

.admin-metrics strong {
  display: block;
  margin-top: 9px;
  font-size: 22px;
  letter-spacing: -.025em;
}

.admin-metrics small {
  display: block;
  margin-top: 6px;
  color: #899189;
  font-size: 10px;
}
```

## Tables

```css
.admin-table {
  width: 100%;
  border-collapse: collapse;
}

.admin-table th {
  padding: 10px 12px;
  text-align: left;
  color: #849087;
  font-size: 9px;
  letter-spacing: .08em;
  text-transform: uppercase;
}

.admin-table td {
  padding: 11px 12px;
  border-top: 1px solid #edf0ec;
  font-size: 11px;
  vertical-align: middle;
}

.admin-table tbody tr:hover {
  background: #f8faf7;
}
```

## Status badges

```css
.admin-status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 5px 8px;
  border-radius: 999px;
  font-size: 9px;
  font-weight: 800;
}

.admin-status.success {
  background: #e8f4ea;
  color: #287047;
}

.admin-status.warning {
  background: #fff2d9;
  color: #806019;
}

.admin-status.danger {
  background: #faeae8;
  color: #9b4941;
}

.admin-status.info {
  background: #edf2f8;
  color: #526b8b;
}
```

No blinking dots.

## Drawers

Use current Richmod drawer interaction.

Desktop width:

```text
min(620px, 100%)
```

Technical sections use compact definition lists.

No raw JSON dump by default.

## Filters

Desktop:

```text
[Status ▼] [Lane ▼] [Type ▼] [24 jam ▼] [Search ID] [Perbarui]
```

On mobile: wrap.

## Time/number formatting

Timezone:

```text
Asia/Jakarta
```

Date:

```text
30 Agu 2026 · 10:14:22
```

Duration:

```text
180ms
1.8s
42s
2m 14s
```

Tokens:

```text
1,420
37k
182k
1.2M
```

Cost:

```text
$0.0042
$0.42
```

Do not convert to IDR without authoritative FX data.

---

# 22. Final Overview design

```text
┌─────────────────────────────────────────────────────────────────────┐
│ ADMINISTRASI PLATFORM                                               │
│ Platform Operations Console                     [Perbarui]         │
│                                                                     │
│ [Overview] [Jobs] [LLM] [Logs] [Households] [Users] [Audit]       │
│                                                                     │
│ ┌────────────┬────────────┬────────────┬────────────┐                │
│ │ PLATFORM   │ WORKER     │ QUEUE      │ FAILED 24H │                │
│ │ Healthy    │ Healthy    │ 3 pending  │ 1          │                │
│ └────────────┴────────────┴────────────┴────────────┘                │
│                                                                     │
│ ┌────────────┬────────────┬────────────┬────────────┐                │
│ │ LLM CALLS  │ SUCCESS    │ REVIEWS    │ HOUSEHOLDS │                │
│ │ 73         │ 98.6%      │ 4 open     │ 2          │                │
│ └────────────┴────────────┴────────────┴────────────┘                │
│                                                                     │
│ ┌───────────────────────────────┬───────────────────────────────┐   │
│ │ JOB QUEUE BY LANE             │ LLM 24H                       │   │
│ │ INTERACTIVE 0 pending         │ Calls       73                 │   │
│ │ DEFAULT     1 pending         │ Failed       1                 │   │
│ │ BACKGROUND  2 pending         │ p95       4.2s                 │   │
│ │                               │ Tokens    182k                 │   │
│ │                               │ Cost      $0.42                │   │
│ └───────────────────────────────┴───────────────────────────────┘   │
│                                                                     │
│ ┌───────────────────────────────────────────────────────────────┐   │
│ │ RECENT OPERATIONAL EVENTS                                    │   │
│ │ WARN  10:04 JOB_RETRY  PROCESS_BANK_EMAIL  TIMEOUT           │   │
│ │ ERROR 10:03 LLM_FAILED BANK_EMAIL            PROVIDER_5XX    │   │
│ │ ERROR 09:52 SOURCE_FAILED BANK_EMAIL                          │   │
│ └───────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────┘
```

---

# 23. Final Jobs design

```text
JOBS

[Status ▼] [Lane ▼] [Type ▼] [24 jam ▼] [Search Job ID] [Perbarui]

┌──────────┬──────────────────────┬────────────┬─────────┬──────────┬────────┐
│ STATUS   │ JOB TYPE             │ LANE       │ ATTEMPT │ DURATION │ UPDATED│
├──────────┼──────────────────────┼────────────┼─────────┼──────────┼────────┤
│ Running  │ PROCESS_BANK_EMAIL   │ Background │ 1 / 5   │ 3.2s     │ now    │
│ Waiting  │ GENERATE_INSIGHT     │ Background │ 0 / 5   │ —        │ 12s    │
│ Failed   │ PROCESS_DOCUMENT     │ Background │ 5 / 5   │ 42s      │ 14m    │
└──────────┴──────────────────────┴────────────┴─────────┴──────────┴────────┘
```

Detail drawer:

```text
Job Detail
PROCESS_BANK_EMAIL
Failed

JOB
ID        6af8…9c31
Lane      BACKGROUND
Attempts  5 / 5

LIFECYCLE
Created   ...
Started   ...
Finished  ...

RETRY HISTORY
#1 TIMEOUT       4.8s
#2 PROVIDER_5XX  3.1s
#3 TIMEOUT       5.0s
```

---

# 24. Final LLM design

```text
LLM MONITORING

[24 jam ▼] [Task ▼] [Status ▼] [Perbarui]

[73 Calls] [98.6% Success] [4.2s P95] [182k Tokens] [$0.42]

┌────────────────────────────────┬──────────────────────────────┐
│ Calls by hour                  │ Gateway                      │
│     ▂▃▆▂▅█...                 │ Configured · responses       │
└────────────────────────────────┴──────────────────────────────┘

Task Breakdown
Task | Calls | Failed | P50 | P95 | Tokens | Cost

Recent Calls
time | task | protocol | model | status | latency | tokens
```

---

# 25. Final Logs design

```text
LOGS
Structured operational events

[Type ▼] [Severity ▼] [24 jam ▼] [Reference ID] [Perbarui]

┌──────────┬──────────┬────────────────┬──────────────────────┬──────────────┐
│ TIME     │ SEVERITY │ TYPE           │ COMPONENT            │ ERROR CLASS  │
│ 10:04:22 │ Warning  │ JOB_RETRY      │ PROCESS_BANK_EMAIL   │ TIMEOUT      │
│ 10:03:58 │ Error    │ LLM_FAILED     │ BANK_EMAIL           │ PROVIDER_5XX │
│ 09:52:08 │ Error    │ SOURCE_FAILED  │ BANK_EMAIL           │ —            │
└──────────┴──────────┴────────────────┴──────────────────────┴──────────────┘
```

---

# 26. Mobile behavior

```text
tabs → horizontal scroll
overview metrics → 2 columns then 1
overview panels → stack
drawers → full-screen
filters → wrap
dense Jobs/LLM/Logs tables → horizontal scroll
Household/User rows → stack if needed
```

Horizontal scrolling is acceptable for dense operational tables.

---

# 27. User mutation hardening

Refactor current `PatchUser`:

```text
load Super Admin principal
begin DB transaction
lock target user
validate final-active-superadmin invariant
apply change
insert platform audit
commit
```

Do not audit secrets.

Current Super Admin add-member action should also create a platform audit row.

---

# 28. Error isolation

Per-tab errors must be local.

Example:

```text
LLM summary fails
→ Overview still shows Worker/Queue
→ LLM panel says "Tidak tersedia"
```

Do not make one failing sub-query blank the entire Admin console if the rest can render.

---

# 29. Empty states

Use compact internal empty states:

```text
Tidak ada job untuk filter ini.
Belum ada panggilan LLM pada rentang ini.
Tidak ada operational event pada rentang ini.
Belum ada household.
Belum ada audit event.
```

No giant illustration.

---

# 30. Accessibility

Require:

```text
tab active state
semantic tables
keyboard-accessible row detail
status text, not color only
labels on filters
accessible confirmations
mobile tab navigation
```

Avoid clickable `<tr>` without keyboard handling.

---

# 31. Backend tests

Required:

Authorization:

```text
non-authenticated blocked
authenticated non-superadmin → 403
superadmin → allowed
```

Overview:

```text
worker healthy/stale
job counts
lane metrics
LLM metrics
null cost
empty data
```

Jobs:

```text
filters
keyset pagination
no payload leakage
retry history
not found
```

LLM:

```text
summary aggregation
task aggregation
failed calls
null cost
pagination
no prompt/output
```

Logs:

```text
retry event normalization
LLM failure normalization
source failure normalization
ordering/pagination
no raw payload
```

Users:

```text
final active Super Admin guard
status audit
role audit
rollback on failure
```

Audit:

```text
platform audit list
household audit list
filters
pagination
```

---

# 32. Frontend tests

Verify:

```text
Admin tabs exist
Overview uses admin overview endpoint
Jobs never renders raw payload_json
LLM does not render prompt/output
privilege changes require confirmation
non-superadmin Admin nav remains hidden
mobile/admin styles exist
```

Pure helpers:

```text
duration formatting
token compaction
status tone mapping
```

---

# 33. Verification

Backend:

```bash
go test ./...
```

Frontend:

```bash
cd apps/web
npm test
npm run build
```

If schema changed:

```text
apply migrations in disposable DB
verify rollback behavior where relevant
```

Manual:

```text
desktop
laptop
tablet
mobile

healthy platform
stale worker
failed jobs
empty jobs
LLM success/failure
zero LLM calls
logs filter
household detail
user privilege confirmation
platform audit
```

---

# 34. Performance acceptance

Admin APIs must be bounded.

Targets under normal deployment load:

```text
overview p95 < 500ms
jobs list p95 < 500ms
llm summary p95 < 800ms
logs list p95 < 800ms
users/households p95 < 500ms
```

Avoid:

```text
N+1
unbounded lists
full-history downloads
heavy OFFSET pagination
```

---

# 35. Out of scope

Do not add:

```text
raw Docker/container log streaming
SSH shell
SQL console
database browser
manual job payload editing
force job success
job deletion
manual arbitrary LLM prompt runner
secret viewer
OAuth token viewer
environment variable viewer
DB credential viewer
financial transaction editing from Admin
household impersonation
login as user
new observability infrastructure
```

Raw centralized log search can be a future ADR/iteration.

---

# 36. Recommended implementation phases

```text
Phase 1
- platform_audit_log migration
- admin backend query structure
- overview/jobs/llm/logs APIs

Phase 2
- Admin tabs + Overview
- Jobs + drawer
- LLM
- Logs

Phase 3
- Households
- Users hardening
- Audit

Phase 4
- EXPLAIN/index tuning
- responsive polish
- tests/build/docs
```

One feature branch/worktree is acceptable.

---

# 37. Definition of Done

Authorization:

```text
[ ] Every new admin API is Super Admin-only.
[ ] Non-superadmin cannot access global operational data.
```

Overview:

```text
[ ] Worker/queue/platform health visible.
[ ] Lane metrics visible.
[ ] LLM 24h summary visible.
[ ] Review/user/household counts visible.
[ ] Recent structured operational events visible.
```

Jobs:

```text
[ ] Filterable/paginated.
[ ] Retry history visible.
[ ] No raw payload exposed.
[ ] No dangerous job mutation buttons.
```

LLM:

```text
[ ] Calls/success/p95/tokens/cost visible.
[ ] Task breakdown works.
[ ] Recent calls paginated.
[ ] No prompt/output/raw input exposed.
[ ] Null cost is not treated as zero.
```

Logs:

```text
[ ] Structured operational events only.
[ ] Retry/LLM/source failures visible.
[ ] Filtering/pagination works.
[ ] No raw stdout persistence added.
```

Households:

```text
[ ] Operational metadata only by default.
[ ] Detail covers members/integrations/operations.
[ ] No default finance amount exposure.
```

Users:

```text
[ ] Existing management moved into Users tab.
[ ] Privilege/status changes require confirmation.
[ ] Final active Super Admin protected.
[ ] Mutations transactional and audited.
```

Audit:

```text
[ ] platform_audit_log exists.
[ ] household audit semantics unchanged.
[ ] Audit filters/pagination work.
[ ] Sensitive values excluded.
```

UI:

```text
[ ] Tabs match Richmod visual language.
[ ] Dense but readable desktop layout.
[ ] Mobile usable.
[ ] Status always textual + color.
[ ] Tables/drawers/filters consistent.
```

Quality:

```text
[ ] Go tests pass.
[ ] Frontend tests pass.
[ ] Web build passes.
[ ] Migration rehearsal passes.
[ ] Query plans reviewed.
[ ] No unrelated financial-state change.
[ ] Worktree workflow follows AGENTS.md.
```

---

# 38. Codex completion report

Report:

```text
baseline main SHA
branch
worktree path

files changed
migration number
new endpoints

Overview status
Jobs status
LLM status
Logs status
Households status
Users status
Audit status

privacy/redaction decisions
query/index changes

backend tests
frontend tests
web build
migration rehearsal
manual desktop/mobile verification

feature commit SHA(s)
merge SHA
pushed main SHA

deployment result only if explicitly requested

remaining limitation:
raw container logs are intentionally not part of V1
```

Do not claim completion if any endpoint exposes raw job payload, raw evidence,
LLM prompt/output, OAuth secrets, session material, or password material.
