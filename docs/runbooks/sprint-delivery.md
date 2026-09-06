# Sprint delivery runbook

## Scope

Use this checklist for every repository-changing Richmod sprint. It is the
operational companion to `AGENTS.md`, CI, ADR-034, and the production deployment
runbook. A sprint is not production-deployed unless the user explicitly asks
for deployment and approves the GitHub `production` Environment gate.

The required sequence is:

```text
latest main
→ isolated branch/worktree
→ inspect + implement + relevant docs
→ local verification
→ commit + feature-branch push
→ no-ff merge + main push
→ main CI + immutable Release Images
→ reclaim disposable local artifacts
→ submit Deploy Production approval
→ user approval
→ deploy + health verification
→ report
```

Never skip a failed or unknown gate. Do not replace an immutable-image release
with a host-local build.

## 1. Preflight

1. Start in the root main worktree. Preserve unrelated changes; never reset,
   clean, or stash them without the owner's approval.
2. Fetch and fast-forward only if the root main worktree is clean enough to do
   so.
3. Read `AGENTS.md`, relevant current ADRs/runbooks, and the code paths being
   changed. User instruction wins over older design documents.
4. Create one descriptive linked worktree from current `main`:

   ```bash
   git worktree add -b <type>/<short-description> \
     ../family-finance-worktrees/<short-description> main
   ```

5. Record base SHA, branch, and worktree path. All edits, tests, image/browser
   caches, temporary databases, and generated files belong in that worktree.

## 2. Implement safely

- Keep the diff scoped to the requested sprint. Do not add future abstractions.
- Update the relevant ADR/runbook/documentation in the same branch when
  behavior, architecture, security boundary, operational procedure, or public
  API changes.
- Keep secrets in host-managed `finance.env` or GitHub Environment secrets.
  Never print, commit, copy, or create an alternate production env file.
- Use disposable fixture data for browser captures/tests. Never use production
  household, transaction, evidence, email, Telegram, or credential data.
- Preserve canonical financial state. Data migration must be new, reversible
  where possible, tested, and never silently repair ambiguous production data.

## 3. Verify before commit

Run the smallest relevant set first, then all checks required by touched areas.
Run commands from the feature worktree.

| Change | Minimum verification |
| --- | --- |
| Go API | `go test ./...` and `go vet ./...` in `apps/api` |
| Go worker | `go test ./...` and `go vet ./...` in `apps/worker` |
| Schema/migration | Apply migrations to disposable PostgreSQL; run affected integration tests |
| Web | `npm test` and `npm run build` in `apps/web` |
| Compose/image files | `docker compose config --quiet`; build affected image(s) |
| Docs/assets | inspect rendered asset(s), links, and `git diff --check` |
| Security/isolation | explicit negative-path and cross-household tests where tenant/auth/input boundaries change |

CI is the final repository-wide authority. Its current matrix includes Gitleaks,
PostgreSQL-backed API/worker tests, Go vet, web build/tests, Compose validation,
and production-image builds. Do not claim checks that were not run.

Before committing:

```bash
git diff --check
git status --short
git diff --stat
```

Inspect the final diff. Confirm generated files are intentional; do not commit
`node_modules`, `.next`, test databases, logs, `.env`, `finance.env`, secrets,
or production evidence.

## 4. Integrate

1. Commit with a descriptive message.
2. Push the feature branch.
3. Fetch `origin/main`. If it advanced, rebase/merge only after checking the
   branch still satisfies the sprint scope and tests.
4. From the clean root main worktree, merge with `--no-ff` and push main:

   ```bash
   git merge --no-ff <feature-branch>
   git push origin main
   ```

5. Record implementation commit, merge commit, and pushed main SHA.

Do not deploy from a feature worktree. Do not merge unrelated dirty root-worktree
files as part of the sprint.

## 5. CI and immutable image gate

After `main` is pushed:

1. Wait for that exact main SHA's `CI` workflow to succeed.
2. Wait for `Release Images` to publish immutable GHCR tags for the same full
   SHA: API, worker, web, and migration.
3. Image publication does **not** deploy. It is the gate that makes local
   artifact reclaim safe.

If CI or Release Images fails, diagnose and fix in a new scoped branch. Do not
submit deployment approval for that SHA.

## 6. Reclaim disposable artifacts

Only after immutable images for the merged SHA exist, reclaim artifacts created
by that sprint:

```bash
git worktree remove ../family-finance-worktrees/<short-description>
git branch -d <feature-branch>
git worktree prune
```

Stop/remove only disposable containers started by the sprint. Delete an image,
browser cache, local build cache, or temporary database only after confirming it
is not shared by another active task. Never use broad cleanup such as `docker
system prune` for a sprint.

Never reclaim production containers, PostgreSQL/attachment/backup volumes,
`/opt/family-finance/finance.env`, or production images in use.

## 7. Submit deployment approval

Only for a user-requested production deployment, manually dispatch `Deploy
Production` with the exact verified pushed main SHA. The workflow itself checks
that SHA remains reachable from `main`, pulls `sha-<full-main-commit>` images,
runs migrations, restarts without local builds, then checks `/healthz` and
`/readyz`.

Submitting the run is not approval. Stop and wait for the user to approve the
GitHub `production` Environment. Never self-approve or imply approval happened.

## 8. Post-approval verification and report

After the approved workflow finishes, record only observed facts:

- exact deploy run and SHA;
- migration result, if any;
- API health and readiness result;
- worker health/result available from workflow/logs;
- relevant sprint smoke test;
- remaining manual/provider cutover steps.

For non-deploy sprints, explicitly report `Deployment: not requested`.

Every handoff reports: branch, worktree, base SHA, implementation commit, merge
commit, pushed main SHA, tests run/results, deployment state, and cleanup state.
Never say a manual production, provider, user-approval, or external verification
step occurred unless it actually occurred.
