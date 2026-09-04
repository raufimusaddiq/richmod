# Richmod Super Admin Console — Finalization Pass

**Repository:** `raufimusaddiq/richmod`  
**Baseline reviewed:** `main` @ `a0a00d32b00ee34938d83b90439e2b82fa64f280`  
**Target:** latest `main` at implementation time  
**Scope:** finish and harden the already-delivered Super Admin Platform Console  
**Stack:** Go + Next.js + React + JavaScript + PostgreSQL + existing Recharts

This is a **finalization pass**, not a redesign and not a new V2.

Preserve the delivered architecture:

```text
Overview
Jobs
LLM
Logs
Households
Users
Audit
```

Preserve existing Super Admin authorization, `platform_audit_log`, job retry history, safe job-reference allowlisting, metadata-only LLM monitoring, structured operational events, transactional admin user mutations, and final-active-superadmin protection.

Do not introduce new observability infrastructure.

---

# 0. Start from latest main

Before editing:

1. pull latest `main`;
2. read `AGENTS.md`;
3. inspect current admin code and newer migrations;
4. use the required branch + linked worktree workflow;
5. do not deploy unless explicitly requested.

Read at minimum:

```text
AGENTS.md

apps/api/internal/admin/handler.go
apps/api/internal/admin/read.go
apps/api/internal/admin/helpers.go
apps/api/cmd/api/main.go
apps/api/internal/platform/httpmw/middleware.go

apps/web/app/admin/page.js
apps/web/app/globals.css
apps/web/tests/product-alignment.test.mjs

db/migrations/00033_platform_admin_console.sql
docs/SUPER_ADMIN_PLATFORM_CONSOLE_CHECKLIST.md
```

---

# 1. Goal

Close the remaining gaps in the current Super Admin Console.

```text
P0
- fix platform-audit request_id correlation
- real pagination for all growing admin lists
- Logs time-range/filter/cursor behavior
- Jobs filters + pagination UI
- LLM recent-calls refresh/pagination/filtering

P1
- Overview recent operational events
- LLM calls-by-hour visualization
- Household operational detail
- Household Audit UI
- human-readable Audit presentation
- keyboard-accessible row actions/drawers
- DB-backed admin integration tests

P2
- admin frontend structure cleanup
- visual polish to better match Richmod
```

---

# 2. P0 — Fix platform audit request_id correlation

Current HTTP middleware can generate request IDs as a 32-character hexadecimal string.

Current platform-audit insert only persists UUID-shaped request IDs, so normal Richmod-generated request IDs can become `NULL`.

Fix this contract.

Preferred:

```text
platform_audit_log.request_id TEXT
```

Add the next migration from latest `main`.

Example:

```sql
ALTER TABLE platform_audit_log
ALTER COLUMN request_id TYPE TEXT
USING request_id::text;
```

Then store the middleware request ID directly.

The same request ID must correlate:

```text
HTTP access log
↔ admin mutation
↔ platform_audit_log
```

Add tests proving the generated request ID survives into platform audit.

---

# 3. P0 — Complete pagination

Jobs already has keyset pagination.

Implement the same real bounded pagination for:

```text
LLM calls
Logs
Platform Audit
Household Audit
```

Requirements:

```text
default limit = 50
max limit = 100
keyset cursor
limit + 1
real nextCursor when more rows exist
stable created_at + id ordering
```

Do not return placeholder `nextCursor` values when pagination is not actually implemented.

Do not use large OFFSET pagination.

---

# 4. P0 — Jobs filters and pagination UI

Backend Jobs already supports filtering.

Expose:

```text
[Status ▼] [Lane ▼] [Job Type ▼] [24 jam ▼] [Search Job ID] [Perbarui]
```

Ranges:

```text
1 jam
24 jam
7 hari
30 hari
```

Prefer URL state:

```text
/admin?tab=jobs&status=FAILED&lane=BACKGROUND&range=24h
```

Use backend filtering, not client-side full-list filtering.

Add `Muat berikutnya` or equivalent cursor pagination.

Keep the existing Job detail drawer.

---

# 5. P0 — Finish Logs

`Logs` remains:

```text
Structured Operational Events
```

Do not implement raw stdout/container logs.

Keep sources:

```text
JOB_RETRY
JOB_FAILED
LLM_FAILED
SOURCE_FAILED
```

Backend filters:

```text
type
severity
component
range
from
to
q/reference
limit
cursor
```

Default:

```text
range=24h
```

Apply time filtering server-side before returning events.

Frontend target:

```text
[Type ▼] [Severity ▼] [Component ▼] [24 jam ▼] [Reference ID] [Perbarui]

Time | Severity | Event | Component | Error Class | Reference

[Muat berikutnya]
```

No raw payload, email/document body, LLM prompt/output, or secret data.

---

# 6. P0 — Finish LLM monitoring

Keep metadata-only monitoring.

Never show:

```text
prompt
input body
document text
Telegram text
tool arguments
model output
provider response
secrets
```

Fix refresh so `Perbarui` refreshes both summary and recent calls.

Add filters:

```text
[24 jam ▼] [Task ▼] [Status ▼] [Perbarui]
```

Add real cursor pagination for recent calls.

Add a small Recharts calls-by-hour bar chart.

For 24h, aggregate per hour with calls and failed count.

Target:

```text
┌────────────────────────────┬────────────────────────────┐
│ Calls by hour              │ Gateway                    │
│ ▂▃▆▂▅█...                 │ Configured · responses     │
└────────────────────────────┴────────────────────────────┘
```

One chart is enough.

---

# 7. P1 — Recent Operational Events on Overview

Add a compact recent-event section to Overview.

Show only the newest 5–8 events.

Example:

```text
WARN   10:04 JOB_RETRY     PROCESS_BANK_EMAIL   TIMEOUT
ERROR  10:03 LLM_FAILED    BANK_EMAIL           PROVIDER_5XX
ERROR  09:52 SOURCE_FAILED BANK_EMAIL
```

Add:

```text
[Lihat Logs →]
```

Prefer including `recentEvents` in `/api/v1/admin/overview` if the query remains cheap and maintainable.

---

# 8. P1 — Finish Household detail

Household detail should contain:

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
open reviews
last source activity
```

Members:

```text
display name
email
role
active status
```

Integrations:

```text
Gmail connected
active bank listeners
Telegram linked identities
primary salary source configured
```

Operations:

```text
recent jobs
recent LLM calls if safely attributable
failed source events
open reviews
```

Recent Audit:

```text
recent household actions
```

Do not show transaction amounts/descriptions by default.

No OAuth tokens/secrets.

---

# 9. P1 — Household Audit UI

Audit page must support:

```text
[Platform] [Household]
```

Platform keeps current platform-audit events.

Household view:

```text
[Household ▼] [Action ▼] [24 jam ▼]

Time | Action | Entity | Request ID
```

Use the existing household audit endpoint, extended with pagination/filtering as needed.

Do not display financial before/after JSON by default.

---

# 10. P1 — Human-readable platform Audit

Do not use raw `JSON.stringify(metadata)` as the primary UI.

Show summaries such as:

```text
User dinonaktifkan
raufi@example.com · aktif → nonaktif

Super Admin diberikan
user@example.com

Member ditambahkan
member@example.com
```

Raw sanitized metadata may exist in a detail drawer if useful.

---

# 11. P1 — Accessibility

Current row interaction must not be mouse-only.

Do not rely only on clickable `<tr>` rows.

Use an accessible button/link in the identifying cell.

Requirements:

```text
keyboard accessible
visible focus
drawer close accessible
Escape closes drawer if practical
focus returns to trigger
semantic tables preserved
```

---

# 12. P1 — DB-backed admin integration tests

Add real integration tests for:

Authorization:

```text
non-superadmin → 403
superadmin → allowed
```

Overview:

```text
healthy worker
stale worker
empty LLM data
```

Jobs:

```text
filters
cursor pagination
retry history
payload redaction
```

LLM:

```text
summary
task/status filters
pagination
null cost
```

Logs:

```text
event normalization
time range
pagination
no raw payload
```

Users:

```text
self-lockout blocked
final active Super Admin blocked
mutation audited
rollback on audit failure
```

Platform audit:

```text
request_id persisted
```

Follow existing DB integration-test conventions.

---

# 13. P2 — Refactor admin frontend

Current admin page is too large.

Split without changing product behavior.

Suggested:

```text
apps/web/app/admin/page.js

apps/web/app/admin/components/
  AdminTabs.js
  OverviewTab.js
  JobsTab.js
  JobDetailDrawer.js
  LLMTab.js
  LogsTab.js
  HouseholdsTab.js
  HouseholdDetailDrawer.js
  UsersTab.js
  AuditTab.js
  AdminTable.js
  AdminBadge.js
```

Do not over-componentize trivial code.

JavaScript only.

---

# 14. P2 — Visual polish

Keep Richmod's visual language.

Do not create a Grafana/devops dark theme.

## Tabs

Current active Admin tab is too dark.

Use:

```text
muted green/grey segmented container
active = white surface
green text
subtle border/shadow
```

Suggested direction:

```css
.admin-tabs {
  display: flex;
  gap: 6px;
  padding: 5px;
  overflow-x: auto;
  border: 1px solid #dfe5df;
  border-radius: 13px;
  background: #eef2ed;
}

.admin-tabs button {
  background: transparent;
  color: #667169;
}

.admin-tabs button.active {
  background: #fff;
  color: #245f40;
  border: 1px solid #dfe5df;
  box-shadow: 0 1px 5px #2443310d;
}
```

## Metrics

Prefer white/surface-like overview metric cards with status color used in values/badges.

Keep tables dense.

Do not turn each table row into a big card.

---

# 15. Error isolation

Each tab owns its own loading/error/empty/refresh state.

Do not leave one global stale error visible after switching tabs.

Example:

```text
LLM request fails
→ only LLM shows error
→ Jobs/Overview remain usable
```

Clear errors after a successful reload.

---

# 16. Privacy rules

Never expose:

```text
job.payload_json
raw last_error
password_hash
session token
OAuth token
API key
database URL
raw bank email
document content
Telegram message
LLM prompt
LLM output
provider raw response
```

Keep Job refs allowlisted only:

```text
source_event_id
document_id
insight_id
review_item_id
```

---

# 17. Performance

All growing APIs must be bounded.

Use:

```text
limit <= 100
time-range defaults
server-side filters
keyset pagination
```

Avoid:

```text
N+1
full-history fetches
large OFFSET
frontend full-dataset filtering
```

Review `EXPLAIN` for changed Logs, LLM, and Audit queries.

Add indexes only when justified.

---

# 18. Final Overview target

```text
ADMINISTRASI PLATFORM
Platform Operations Console

[Overview] [Jobs] [LLM] [Logs] [Households] [Users] [Audit]

[Platform] [Worker] [Queue] [Failed 24h]
[LLM Calls] [Success] [Reviews] [Households]

┌─────────────────────────────┬────────────────────────────┐
│ Queue by lane               │ LLM 24h                    │
│ INTERACTIVE ...             │ success / p95 / tokens     │
│ DEFAULT ...                 │ gateway                    │
│ BACKGROUND ...              │                            │
└─────────────────────────────┴────────────────────────────┘

┌──────────────────────────────────────────────────────────┐
│ Recent Operational Events                                │
│ WARN  JOB_RETRY ...                                      │
│ ERROR LLM_FAILED ...                                     │
│                                      [Lihat Logs →]      │
└──────────────────────────────────────────────────────────┘
```

---

# 19. Final Jobs target

```text
JOBS

[Status ▼] [Lane ▼] [Type ▼] [24 jam ▼] [Search ID] [Perbarui]

Status | Type | Lane | Attempts | Duration | Updated | Job ID

[Muat berikutnya]
```

---

# 20. Final LLM target

```text
LLM

[24 jam ▼] [Task ▼] [Status ▼] [Perbarui]

[Calls] [Success] [P95] [Tokens] [Cost]

┌────────────────────────────┬────────────────────────────┐
│ Calls by hour              │ Gateway                    │
│ bar chart                  │ Configured · responses     │
└────────────────────────────┴────────────────────────────┘

Task Breakdown

Recent Calls

[Muat berikutnya]
```

---

# 21. Final Logs target

```text
LOGS
Structured operational events

[Type ▼] [Severity ▼] [Component ▼] [24 jam ▼] [Reference] [Perbarui]

Time | Severity | Event | Component | Error Class | Reference

[Muat berikutnya]
```

---

# 22. Final Audit target

```text
AUDIT

[Platform] [Household]

Platform:
Time | Action | Actor | Entity | Summary | Request ID

Household:
[Household ▼] [Action ▼] [24 jam ▼]

Time | Action | Entity | Request ID
```

No raw JSON as primary content.

---

# 23. Update checklist

Update:

```text
docs/SUPER_ADMIN_PLATFORM_CONSOLE_CHECKLIST.md
```

Only mark items complete after verification.

Close truthfully:

```text
DB-backed integration tests
desktop/tablet/mobile keyboard review
real shared pagination
query-plan verification
```

---

# 24. Verification

Backend:

```bash
go test ./...
go vet ./...
```

Frontend:

```bash
cd apps/web
npm test
npm run build
```

Migration:

```text
apply against disposable PostgreSQL
verify request_id migration
verify platform audit read/write
```

Manual browser review:

```text
desktop
laptop
tablet
mobile
keyboard-only
```

Test:

```text
Overview healthy/stale
Jobs filters/pagination
Job drawer
LLM filters/pagination/refresh/chart
Logs filters/pagination
Household detail
Platform Audit
Household Audit
user privilege confirmation
request ID correlation
```

---

# 25. Definition of Done

```text
[ ] platform audit preserves normal Richmod request IDs.

[ ] Jobs filters work end-to-end.
[ ] Jobs pagination works end-to-end.

[ ] LLM Calls pagination works.
[ ] LLM range/task/status filters work.
[ ] LLM refresh updates summary + calls.
[ ] LLM calls-by-hour chart exists.

[ ] Logs defaults to bounded time range.
[ ] Logs filters work.
[ ] Logs pagination works.

[ ] Platform Audit pagination works.
[ ] Household Audit pagination works.
[ ] Audit UI supports Platform + Household.
[ ] Audit table uses human-readable summary.

[ ] Overview shows recent operational events.

[ ] Household detail includes members/integrations/operations/recent audit.

[ ] Row detail interactions are keyboard accessible.

[ ] DB-backed integration tests cover admin endpoints and final-admin invariant.

[ ] No raw payload/content/secrets are exposed.

[ ] Admin UI stays consistent with Richmod style.
[ ] Admin active tab uses lighter segmented styling.

[ ] go test ./... passes.
[ ] go vet ./... passes.
[ ] npm test passes.
[ ] npm run build passes.

[ ] migrations apply cleanly.
[ ] query plans reviewed.
[ ] responsive + keyboard review completed.
[ ] checklist updated truthfully.
[ ] worktree workflow follows AGENTS.md.
```

---

# 26. Completion report

Report:

```text
baseline main SHA
branch
worktree path

files changed
migration number
request-id fix

pagination implementation by endpoint
Jobs filters
Logs filters
LLM filters/chart/refresh
Overview recent events
Household detail
Audit UI

privacy/redaction verification

backend tests
frontend tests
go vet
web build
migration rehearsal
query-plan review
manual responsive/keyboard review

feature commit SHA(s)
merge SHA
pushed main SHA

remaining caveats
deployment status only if explicitly requested
```

Do not claim final completion if any growing list still returns a fake/empty `nextCursor` instead of real bounded pagination.
