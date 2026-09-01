# Admin Mobile Table and Audit Fix Checklist

Baseline: `main` at `99263e8`.

- [x] Keep desktop admin table layout unchanged.
- [x] Reduce mobile table-card text spacing and line height.
- [x] Add consistent spacing and padding between mobile table cards on every admin tab.
- [x] Confirm production platform audit source is empty while household audit contains events.
- [x] Add bounded combined audit feed without removing Platform or Household scoped filters.
- [x] Keep audit output redacted to existing safe fields.
- [x] Verify combined SQL against production-shaped PostgreSQL data.
- [x] Pass frontend tests and production build.
- [x] Pass API tests and vet.

No schema migration or new dependency required.
