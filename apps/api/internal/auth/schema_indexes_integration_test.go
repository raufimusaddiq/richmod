package auth

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// redundantIndexesSQL lists every public index another index on the same
// table already implies (migration 00089):
//   - a non-unique btree whose key columns are a leading prefix of (or equal
//     to) another btree's, with the same predicate, opclasses, collations and
//     sort options and no expressions; for two plain duplicates the
//     lower-named one is reported;
//   - a unique btree implied by a non-partial unique btree on a subset of its
//     columns, unless it is a foreign-key target or the primary key.
//
// The composite (household_id, id) keys are foreign-key targets and so are
// excluded by design.
const redundantIndexesSQL = `
WITH ix AS (
  SELECT i.indexrelid, i.indrelid, c.relname idx, am.amname,
         i.indisunique uniq, i.indisprimary pk, i.indnullsnotdistinct nnd,
         string_to_array(i.indkey::text,' ')::int[] cols, i.indnkeyatts nkey,
         string_to_array(i.indclass::text,' ')::oid[] cls,
         string_to_array(i.indcollation::text,' ')::oid[] coll,
         string_to_array(i.indoption::text,' ')::int[] opt,
         coalesce(pg_get_expr(i.indpred,i.indrelid),'') pred,
         i.indexprs IS NULL noexpr,
         EXISTS(SELECT 1 FROM pg_constraint k WHERE k.conindid=i.indexrelid) backs_con,
         EXISTS(SELECT 1 FROM pg_constraint f WHERE f.contype='f' AND f.conindid=i.indexrelid) fk_target
  FROM pg_index i
  JOIN pg_class c ON c.oid=i.indexrelid
  JOIN pg_class t ON t.oid=i.indrelid
  JOIN pg_namespace n ON n.oid=t.relnamespace
  JOIN pg_am am ON am.oid=c.relam
  WHERE n.nspname='public' AND am.amname='btree')
SELECT DISTINCT d.idx || ' (implied by ' || k.idx || ')'
FROM ix d JOIN ix k ON k.indrelid=d.indrelid AND k.indexrelid<>d.indexrelid
WHERE d.noexpr AND k.noexpr AND NOT d.pk AND (
  (NOT d.uniq AND NOT d.backs_con AND d.pred=k.pred AND d.nkey<=k.nkey
   AND d.cols[1:d.nkey]=k.cols[1:d.nkey] AND d.cls[1:d.nkey]=k.cls[1:d.nkey]
   AND d.coll[1:d.nkey]=k.coll[1:d.nkey] AND d.opt[1:d.nkey]=k.opt[1:d.nkey]
   AND (cardinality(d.cols)=d.nkey OR d.cols[d.nkey+1:] <@ k.cols)
   AND NOT (d.nkey=k.nkey AND cardinality(d.cols)=cardinality(k.cols)
            AND NOT (k.uniq OR k.backs_con) AND d.idx<k.idx))
  OR
  (d.uniq AND NOT d.fk_target AND k.uniq AND k.pred=''
   AND k.cols[1:k.nkey] <@ d.cols[1:d.nkey] AND (NOT d.nnd OR k.nnd))
)
ORDER BY 1`

// TestSchemaHasNoRedundantIndexes keeps indexes that another index already
// implies from being added again: each one slows every write for no read.
func TestSchemaHasNoRedundantIndexes(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not configured")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	rows, err := pool.Query(ctx, redundantIndexesSQL)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var redundant []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		redundant = append(redundant, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(redundant) > 0 {
		t.Fatalf("redundant indexes; drop them or document why they stay:\n  %s", strings.Join(redundant, "\n  "))
	}
}
