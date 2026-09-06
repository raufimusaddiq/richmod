# Disposable test matrix

## Purpose

Use this matrix to verify a sprint without touching production services, data,
credentials, or long-lived local state. Every disposable resource must have a
unique task-scoped name, contain test-only data, and be removable independently
from other worktrees or running applications.

This matrix complements the required workflow in `AGENTS.md`. Run it inside the
sprint's linked feature worktree, never the production `main` worktree.

## Select the matrix rows

Run every row affected by the diff. Cross-cutting financial, tenant, auth,
migration, queue, email, Telegram, or document changes run both API and worker
rows even when most edits live in one module.

| Diff scope | Required disposable rows |
| --- | --- |
| Docs only | D0 |
| Web UI only | D0, D3 |
| API without DB behavior | D0, D2 API |
| Worker without DB behavior | D0, D2 worker |
| Migration, financial state, tenant/auth, queue/job behavior | D0, D1, D2 API + worker |
| Compose or Dockerfile | D0, D4; D5 for affected images |
| README browser screenshots | D0, D3, D6 |
| Cross-stack release | D0 through D5; D6 when screenshots changed |

CI remains mandatory after merge. Local row selection saves time; it does not
weaken CI or release-image gates.

Create one run identifier before running any row that creates Docker resources.
Reuse it across D1, D5, and cleanup; never use a generic name such as `test-db`
that could collide with another task.

```bash
TEST_RUN_ID="$(date +%Y%m%d%H%M%S)-$$"
```

## D0 — Diff and secret boundary

```bash
git status --short
git diff --check
git diff --stat
git diff
```

Confirm the diff excludes `.env`, `finance.env`, tokens, raw financial evidence,
real email content, production exports, `node_modules`, `.next`, and test logs.
Gitleaks runs again in GitHub CI.

## D1 — Isolated PostgreSQL 17

Keep these commands in one terminal so the variables and exit trap remain
active. The random run suffix prevents collision with other agents/sprints.

```bash
set -eu

# Set once for this terminal; keep the same value through cleanup.
TEST_RUN_ID="${TEST_RUN_ID:-$(date +%Y%m%d%H%M%S)-$$}"
TEST_NETWORK="richmod-test-${TEST_RUN_ID}"
TEST_DB_CONTAINER="richmod-test-db-${TEST_RUN_ID}"
TEST_DATABASE_URL="postgres://richmod_test:test-only-password@db:5432/richmod_test?sslmode=disable"

cleanup_test_matrix() {
  docker rm -f "$TEST_DB_CONTAINER" >/dev/null 2>&1 || true
  docker network rm "$TEST_NETWORK" >/dev/null 2>&1 || true
}
trap cleanup_test_matrix EXIT INT TERM

docker network create "$TEST_NETWORK"
docker run --rm -d \
  --name "$TEST_DB_CONTAINER" \
  --network "$TEST_NETWORK" \
  --network-alias db \
  -e POSTGRES_DB=richmod_test \
  -e POSTGRES_USER=richmod_test \
  -e POSTGRES_PASSWORD=test-only-password \
  --health-cmd='pg_isready -U richmod_test -d richmod_test' \
  --health-interval=1s \
  --health-timeout=3s \
  --health-retries=60 \
  postgres:17.4-alpine

for attempt in $(seq 1 60); do
  [ "$(docker inspect --format '{{.State.Health.Status}}' "$TEST_DB_CONTAINER")" = healthy ] && break
  sleep 1
done

[ "$(docker inspect --format '{{.State.Health.Status}}' "$TEST_DB_CONTAINER")" = healthy ]
```

This database starts empty, has no host port, joins only its unique Docker
network, and disappears when the trap runs. Never use production PostgreSQL or
copy production rows into it.

## D2 — Migrations, API, and worker

Run after D1 in the same terminal. The Go container joins only the disposable
network. Module/build caches stay inside the disposable container.

Full backend matrix:

```bash
docker run --rm \
  --network "$TEST_NETWORK" \
  -e TEST_DATABASE_URL="$TEST_DATABASE_URL" \
  -v "$PWD:/src" -w /src \
  golang:1.25.0-bookworm \
  sh -ec '
    go install github.com/pressly/goose/v3/cmd/goose@v3.24.1
    goose -dir db/migrations postgres "$TEST_DATABASE_URL" up
    (cd apps/api && go test ./... && go vet ./...)
    (cd apps/worker && go test ./... && go vet ./...)
  '
```

For a narrow non-DB change, the applicable module can run alone without D1:

```bash
docker run --rm -v "$PWD:/src" -w /src/apps/api golang:1.25.0-bookworm \
  sh -ec 'go test ./... && go vet ./...'

docker run --rm -v "$PWD:/src" -w /src/apps/worker golang:1.25.0-bookworm \
  sh -ec 'go test ./... && go vet ./...'
```

Integration tests skip when `TEST_DATABASE_URL` is absent. Therefore a passing
narrow command is not evidence that DB integration tests ran. Migration,
financial, tenant, queue, or persistence work must use the full D1+D2 matrix.

For a migration sprint, verify `goose status` after `up`. Test `down` then `up`
only when that migration deliberately supports rollback and the sprint requires
it; never infer rollback safety from a successful forward migration.

## D3 — Web tests and production build

Mount the repository root because product-alignment tests read API source files.
Anonymous volumes keep `node_modules` and `.next` out of the worktree and are
removed with the container.

```bash
docker run --rm \
  -v "$PWD:/src" \
  -v /src/apps/web/node_modules \
  -v /src/apps/web/.next \
  -w /src/apps/web \
  -e NEXT_TELEMETRY_DISABLED=1 \
  node:22.14-alpine \
  sh -ec 'npm ci --ignore-scripts --no-audit --no-fund && npm run build && npm test'
```

Do not mount only `apps/web`; that makes repository-spanning tests fail for the
wrong reason.

## D4 — Compose validation

Use test-only placeholders. This parses the Compose graph without starting the
production stack.

```bash
POSTGRES_DB=richmod_test \
POSTGRES_USER=richmod_test \
POSTGRES_PASSWORD=test-only-password \
DATABASE_URL='postgres://richmod_test:test-only-password@db:5432/richmod_test?sslmode=disable' \
SESSION_SECRET='12345678901234567890123456789012' \
docker compose config --quiet
```

## D5 — Disposable image-build matrix

Run only for Dockerfile, build-context, dependency, or Compose image changes.
Use a unique prefix; never overwrite production or normal local tags.

```bash
TEST_IMAGE_PREFIX="richmod-test-${TEST_RUN_ID}"

docker build -f infra/docker/api.Dockerfile -t "${TEST_IMAGE_PREFIX}-api" .
docker build -f infra/docker/worker.Dockerfile -t "${TEST_IMAGE_PREFIX}-worker" .
docker build -f infra/docker/migrate.Dockerfile -t "${TEST_IMAGE_PREFIX}-migrate" .
docker build -f infra/docker/web.Dockerfile -t "${TEST_IMAGE_PREFIX}-web" .
docker build -f infra/docker/backup.Dockerfile -t "${TEST_IMAGE_PREFIX}-backup" .
```

Remove only those exact tags after recording results:

```bash
docker image rm \
  "${TEST_IMAGE_PREFIX}-api" \
  "${TEST_IMAGE_PREFIX}-worker" \
  "${TEST_IMAGE_PREFIX}-migrate" \
  "${TEST_IMAGE_PREFIX}-web" \
  "${TEST_IMAGE_PREFIX}-backup"
```

Do not run `docker system prune` or delete shared BuildKit caches. CI rebuilds
all five images independently before release images are published.

## D6 — Disposable Playwright capture

For README images, use the synthetic browser/API fixtures and cleanup procedure
in [the README visual showcase runbook](readme-showcase.md). Never reuse a
production session, URL, or financial dataset.

## Finish and prove cleanup

Let the D1 trap run or invoke it explicitly:

```bash
cleanup_test_matrix
trap - EXIT INT TERM
```

Verify no task-scoped container/network remains and the worktree contains only
intended source changes:

```bash
docker ps -a --filter "name=richmod-test-${TEST_RUN_ID}"
docker network ls --filter "name=${TEST_NETWORK}"
git status --short
git diff --check
```

Expected container/network output is empty. Report each row run, its exact
command/result, skipped rows with scope reason, and cleanup result. Never report
an integration test as run when it was skipped because `TEST_DATABASE_URL` was
unset.
