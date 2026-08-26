package salary

import (
	"encoding/json"
	"net/http"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/raufimusaddiq/richmod/apps/api/internal/auth"
)

type Handler struct{ pool *pgxpool.Pool }
func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }
func (h *Handler) Sources(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFromContext(r.Context()); if !ok || len(p.Memberships)==0 { http.Error(w, `{"error":"household membership required"}`,403); return }
	household:=p.Memberships[0].HouseholdID
	if r.Method==http.MethodPost {
		var body struct{ SourceID string `json:"sourceId"` }; if json.NewDecoder(r.Body).Decode(&body)!=nil || body.SourceID=="" { http.Error(w, `{"error":"sourceId required"}`,400); return }
		if p.Memberships[0].Role!="OWNER" { http.Error(w, `{"error":"owner access required"}`,403); return }
		tx,err:=h.pool.Begin(r.Context()); if err!=nil { http.Error(w,`{"error":"unable to update salary source"}`,500); return }; defer tx.Rollback(r.Context())
		if _,err=tx.Exec(r.Context(),`UPDATE salary_source SET is_primary=false,updated_at=now() WHERE household_id=$1 AND active; UPDATE salary_source SET is_primary=true,updated_at=now() WHERE id=$2 AND household_id=$1 AND active`,household,body.SourceID); err!=nil || tx.Commit(r.Context())!=nil { http.Error(w,`{"error":"unable to update salary source"}`,500); return }
		if _,err=h.pool.Exec(r.Context(),`INSERT INTO audit_log(household_id,actor_type,actor_id,action,entity_type,entity_id,after_json) VALUES($1,'USER',$2,'SELECT_PRIMARY_SALARY_SOURCE','salary_source',$3,jsonb_build_object('is_primary',true))`,household,p.UserID,body.SourceID); err!=nil { http.Error(w,`{"error":"unable to audit salary source"}`,500); return }
	}
	rows,err:=h.pool.Query(r.Context(),`SELECT id,employer,is_primary,active FROM salary_source WHERE household_id=$1 ORDER BY is_primary DESC,employer`,household); if err!=nil { http.Error(w,`{"error":"unable to load salary sources"}`,500); return }; defer rows.Close()
	out:=make([]map[string]any,0); for rows.Next(){ var id,employer string; var primary,active bool; if rows.Scan(&id,&employer,&primary,&active)==nil { out=append(out,map[string]any{"id":id,"employer":employer,"isPrimary":primary,"active":active}) } }; json.NewEncoder(w).Encode(out)
}
