package settings

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

// TestCreateCategoryRejectsAnotherHouseholdsParent: a category can only nest
// under a parent of the caller's own household.
func TestCreateCategoryRejectsAnotherHouseholdsParent(t *testing.T) {
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
	stamp := time.Now().UnixNano()
	var own, other, ownParent, otherParent string
	for _, target := range []*string{&own, &other} {
		if err := pool.QueryRow(ctx, `INSERT INTO household(name) VALUES($1) RETURNING id`, fmt.Sprintf("Category scope %d", stamp)).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Rumah','rumah') RETURNING id`, own).Scan(&ownParent); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO category(household_id,name,slug) VALUES($1,'Rahasia','rahasia') RETURNING id`, other).Scan(&otherParent); err != nil {
		t.Fatal(err)
	}
	principal := auth.Principal{UserID: "00000000-0000-4000-8000-000000000001", HasHousehold: true, HouseholdID: own, HouseholdRole: "OWNER"}
	create := func(parent string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"name":"Listrik","slug":"listrik-%d","parentId":%q}`, time.Now().UnixNano(), parent)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/categories", bytes.NewBufferString(body))
		request = request.WithContext(auth.ContextWithPrincipal(ctx, principal))
		response := httptest.NewRecorder()
		NewHandler(pool).Categories(response, request)
		return response
	}
	if response := create(otherParent); response.Code != http.StatusBadRequest {
		t.Fatalf("foreign parent: %d %s", response.Code, response.Body.String())
	}
	var leaked int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM category WHERE parent_id=$1 AND household_id=$2`, otherParent, own).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatalf("cross-household child rows = %d (%v)", leaked, err)
	}
	if response := create(ownParent); response.Code != http.StatusCreated {
		t.Fatalf("own parent: %d %s", response.Code, response.Body.String())
	}
}
