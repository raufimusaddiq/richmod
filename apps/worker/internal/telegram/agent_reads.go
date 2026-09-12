package telegram

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"
)

func (p *Processor) executeAgentRead(ctx context.Context, state *agentState, call gateway.ToolCall, args map[string]any) (agentToolResult, error) {
	result := agentToolResult{CallID: call.CallID, Tool: call.Name, Class: agentToolRead, Status: "OK"}
	switch call.Name {
	case "query_spending":
		r, err := p.resolveAgentRange(ctx, state.HouseholdID, state.Now, args); if err != nil { return result, err }
		var total, count string
		if err := p.pool.QueryRow(ctx, `SELECT COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0)::text,count(*) FILTER(WHERE type IN('EXPENSE','REFUND'))::text FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND transaction_at >= $2 AND transaction_at < $3`, state.HouseholdID, r.From, r.To).Scan(&total, &count); err != nil { return result, err }
		result.Facts = map[string]any{"period": agentPeriodFact(r), "net_expense_idr": total, "transaction_count": count}
	case "query_cashflow":
		r, err := p.resolveAgentRange(ctx, state.HouseholdID, state.Now, args); if err != nil { return result, err }
		var income, expense, net string
		if err := p.pool.QueryRow(ctx, `SELECT COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)::text,COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0)::text,(COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)-COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0))::text FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND transaction_at >= $2 AND transaction_at < $3`, state.HouseholdID, r.From, r.To).Scan(&income, &expense, &net); err != nil { return result, err }
		result.Facts = map[string]any{"period": agentPeriodFact(r), "income_idr": income, "expense_idr": expense, "net_cashflow_idr": net}
	case "query_savings":
		r, err := p.resolveAgentRange(ctx, state.HouseholdID, state.Now, args); if err != nil { return result, err }
		var total, count string
		if err := p.pool.QueryRow(ctx, `SELECT COALESCE(sum(amount),0)::text,count(*)::text FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND type='TRANSFER' AND purpose IN ('SAVINGS_TRANSFER','INVESTMENT_CONTRIBUTION','ASSET_PURCHASE') AND transaction_at >= $2 AND transaction_at < $3`, state.HouseholdID, r.From, r.To).Scan(&total, &count); err != nil { return result, err }
		result.Facts = map[string]any{"period": agentPeriodFact(r), "savings_idr": total, "transaction_count": count}
	case "get_category_breakdown":
		r, err := p.resolveAgentRange(ctx, state.HouseholdID, state.Now, args); if err != nil { return result, err }
		rows, err := p.pool.Query(ctx, `SELECT COALESCE(c.slug,''),COALESCE(c.name,'Tanpa kategori'),sum(CASE WHEN t.type='EXPENSE' THEN t.amount ELSE -t.amount END)::text,count(*)::text FROM transaction t LEFT JOIN category c ON c.id=t.category_id WHERE t.household_id=$1 AND t.status='CONFIRMED' AND t.type IN('EXPENSE','REFUND') AND t.transaction_at >= $2 AND t.transaction_at < $3 GROUP BY COALESCE(c.slug,''),COALESCE(c.name,'Tanpa kategori') HAVING sum(CASE WHEN t.type='EXPENSE' THEN t.amount ELSE -t.amount END)>0 ORDER BY sum(CASE WHEN t.type='EXPENSE' THEN t.amount ELSE -t.amount END) DESC LIMIT 20`, state.HouseholdID, r.From, r.To); if err != nil { return result, err }
		defer rows.Close()
		items := make([]map[string]any, 0, 20)
		for rows.Next() { var slug,name,amount,count string; if err := rows.Scan(&slug,&name,&amount,&count); err != nil { return result, err }; items=append(items,map[string]any{"slug":slug,"name":name,"amount_idr":amount,"transaction_count":count}) }
		if err := rows.Err(); err != nil { return result, err }
		result.Facts = map[string]any{"period": agentPeriodFact(r), "categories": items}
	case "get_largest_transactions":
		r, err := p.resolveAgentRange(ctx, state.HouseholdID, state.Now, args); if err != nil { return result, err }
		limit := 5; if v, ok := args["limit"].(float64); ok && int(v)>=1 && int(v)<=10 { limit=int(v) }
		rows, err := p.pool.Query(ctx, `SELECT t.id,t.transaction_at,t.type,t.amount::text,COALESCE(t.counterparty_name,t.description,c.name,'Transaksi') FROM transaction t LEFT JOIN category c ON c.id=t.category_id WHERE t.household_id=$1 AND t.status='CONFIRMED' AND t.type IN('EXPENSE','REFUND') AND t.transaction_at >= $2 AND t.transaction_at < $3 ORDER BY t.amount DESC,t.transaction_at DESC LIMIT $4`, state.HouseholdID, r.From, r.To, limit); if err != nil { return result, err }
		defer rows.Close()
		ids := make([]string,0,limit); items:=make([]map[string]any,0,limit); refs:=make([]agentPublicRef,0,limit)
		for rows.Next(){ var id,typ,amount,label string; var at time.Time; if err:=rows.Scan(&id,&at,&typ,&amount,&label); err!=nil{return result,err}; ids=append(ids,id); ref:=fmt.Sprintf("tx_%d",len(ids)); items=append(items,map[string]any{"ref":ref,"transaction_at":at.In(jakartaLocation()).Format(time.RFC3339),"type":typ,"amount_idr":amount,"label":label}); refs=append(refs,agentPublicRef{Ref:ref,Type:"TRANSACTION",Label:label}) }
		if err:=rows.Err();err!=nil{return result,err}
		if len(ids)>0 { if err:=p.persistTransactionReferences(ctx,state.HouseholdID,state.SourceEventID,state.Update,ids);err!=nil{return result,err} }
		result.Facts=map[string]any{"period":agentPeriodFact(r),"transactions":items};result.References=refs
	case "search_transactions":
		r, err := p.resolveAgentRange(ctx, state.HouseholdID, state.Now, args); if err != nil { return result, err }
		query,_:=args["search_text"].(string); query=clean(query,120); if query=="" { return result, fmt.Errorf("empty search") }
		rows,err:=p.pool.Query(ctx,`SELECT t.id,t.transaction_at,t.type,t.amount::text,COALESCE(t.counterparty_name,t.description,c.name,'Transaksi') FROM transaction t LEFT JOIN category c ON c.id=t.category_id WHERE t.household_id=$1 AND t.status<>'VOIDED' AND t.type IN('INCOME','EXPENSE','REFUND') AND t.transaction_at >= $2 AND t.transaction_at < $3 AND (t.counterparty_name ILIKE '%'||$4||'%' OR t.description ILIKE '%'||$4||'%' OR c.name ILIKE '%'||$4||'%') ORDER BY t.transaction_at DESC LIMIT 10`,state.HouseholdID,r.From,r.To,query);if err!=nil{return result,err};defer rows.Close()
		ids:=make([]string,0,10);items:=make([]map[string]any,0,10);refs:=make([]agentPublicRef,0,10)
		for rows.Next(){var id,typ,amount,label string;var at time.Time;if err:=rows.Scan(&id,&at,&typ,&amount,&label);err!=nil{return result,err};ids=append(ids,id);ref:=fmt.Sprintf("tx_%d",len(ids));items=append(items,map[string]any{"ref":ref,"transaction_at":at.In(jakartaLocation()).Format(time.RFC3339),"type":typ,"amount_idr":amount,"label":label});refs=append(refs,agentPublicRef{Ref:ref,Type:"TRANSACTION",Label:label})}
		if err:=rows.Err();err!=nil{return result,err};if len(ids)>0{if err:=p.persistTransactionReferences(ctx,state.HouseholdID,state.SourceEventID,state.Update,ids);err!=nil{return result,err}}
		result.Facts=map[string]any{"query":query,"period":agentPeriodFact(r),"transactions":items,"truncated":len(items)==10};result.References=refs
	case "get_transaction_details":
		ref,_:=args["target_ref"].(string);id,err:=p.resolveTransactionReference(ctx,state.HouseholdID,state.Update,ref);if err!=nil{return result,fmt.Errorf("transaction reference unavailable: %w",err)}
		var at time.Time;var typ,status,amount,category,description,merchant string
		if err:=p.pool.QueryRow(ctx,`SELECT t.transaction_at,t.type,t.status,t.amount::text,COALESCE(c.name,''),COALESCE(t.description,''),COALESCE(t.counterparty_name,'') FROM transaction t LEFT JOIN category c ON c.id=t.category_id WHERE t.id=$1 AND t.household_id=$2 AND t.status<>'VOIDED'`,id,state.HouseholdID).Scan(&at,&typ,&status,&amount,&category,&description,&merchant);err!=nil{return result,err}
		result.Facts=map[string]any{"ref":ref,"transaction_at":at.In(jakartaLocation()).Format(time.RFC3339),"type":typ,"status":status,"amount_idr":amount,"category":category,"description":description,"merchant":merchant}
	case "query_wealth":
		var observed time.Time;var value string
		err:=p.pool.QueryRow(ctx,`SELECT s.observed_at,COALESCE(sum(CASE WHEN w.side='LIABILITY' THEN -i.value_idr ELSE i.value_idr END),0)::text FROM wealth_snapshot s JOIN wealth_snapshot_item i ON i.snapshot_id=s.id JOIN wealth_account w ON w.id=i.wealth_account_id WHERE s.household_id=$1 AND s.id=(SELECT id FROM wealth_snapshot WHERE household_id=$1 ORDER BY observed_at DESC,id DESC LIMIT 1) GROUP BY s.id,s.observed_at`,state.HouseholdID).Scan(&observed,&value)
		if errors.Is(err,pgx.ErrNoRows){result.Status="NO_DATA";result.Facts=map[string]any{"has_snapshot":false};return result,nil};if err!=nil{return result,err}
		result.Facts=map[string]any{"has_snapshot":true,"net_worth_idr":value,"observed_at":observed.In(jakartaLocation()).Format(time.RFC3339)}
	case "list_wealth_accounts":
		rows,err:=p.pool.Query(ctx,`SELECT name,COALESCE(institution,''),usage_role,side FROM wealth_account WHERE household_id=$1 AND active ORDER BY name,id`,state.HouseholdID);if err!=nil{return result,err};defer rows.Close();items:=[]map[string]any{}
		for rows.Next(){var name,institution,role,side string;if err:=rows.Scan(&name,&institution,&role,&side);err!=nil{return result,err};items=append(items,map[string]any{"name":name,"institution":institution,"usage_role":role,"side":side})};if err:=rows.Err();err!=nil{return result,err};result.Facts=map[string]any{"accounts":items}
	case "list_review_items":
		rows,err:=p.pool.Query(ctx,`SELECT r.review_type,t.amount::text,COALESCE(t.counterparty_name,t.description,'Transaksi') FROM review_request r JOIN transaction t ON t.id=r.transaction_id WHERE r.household_id=$1 AND r.status IN('PENDING_SEND','OPEN') ORDER BY r.created_at DESC LIMIT 10`,state.HouseholdID);if err!=nil{return result,err};defer rows.Close();items:=[]map[string]any{}
		for rows.Next(){var kind,amount,label string;if err:=rows.Scan(&kind,&amount,&label);err!=nil{return result,err};items=append(items,map[string]any{"review_type":kind,"amount_idr":amount,"label":label})};if err:=rows.Err();err!=nil{return result,err};result.Facts=map[string]any{"reviews":items,"count":len(items)}
	case "get_finance_insight":
		r,err:=p.resolveAgentRange(ctx,state.HouseholdID,state.Now,args);if err!=nil{return result,err};var income,expense,net,reviews string
		if err:=p.pool.QueryRow(ctx,`SELECT COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)::text,COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0)::text,(COALESCE(sum(amount) FILTER(WHERE type='INCOME'),0)-COALESCE(sum(CASE WHEN type='EXPENSE' THEN amount WHEN type='REFUND' THEN -amount ELSE 0 END),0))::text,(SELECT count(*)::text FROM transaction WHERE household_id=$1 AND status='NEEDS_REVIEW') FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND transaction_at >= $2 AND transaction_at < $3`,state.HouseholdID,r.From,r.To).Scan(&income,&expense,&net,&reviews);err!=nil{return result,err}
		var topName,topAmount string;_ = p.pool.QueryRow(ctx,`SELECT COALESCE(counterparty_name,description,'Tidak diketahui'),sum(amount)::text FROM transaction WHERE household_id=$1 AND status='CONFIRMED' AND type='EXPENSE' AND transaction_at >= $2 AND transaction_at < $3 GROUP BY 1 ORDER BY sum(amount) DESC LIMIT 1`,state.HouseholdID,r.From,r.To).Scan(&topName,&topAmount)
		result.Facts=map[string]any{"period":agentPeriodFact(r),"income_idr":income,"expense_idr":expense,"net_cashflow_idr":net,"open_review_count":reviews,"largest_counterparty":topName,"largest_counterparty_expense_idr":topAmount}
	default:
		return result,fmt.Errorf("unsupported read tool %q",call.Name)
	}
	return result,nil
}

func (p *Processor) resolveAgentRange(ctx context.Context, householdID string, now time.Time, args map[string]any) (assistantRange,error){
	period,_:=args["period"].(string);from,_:=args["from_date"].(string);to,_:=args["to_date"].(string)
	if period=="CURRENT_CYCLE"||period=="PREVIOUS_CYCLE"{return p.resolveSalaryCycleRange(ctx,householdID,now,period=="PREVIOUS_CYCLE")}
	return resolveAssistantRange(now,&period,stringPtr(from),stringPtr(to))
}

func agentPeriodFact(r assistantRange) map[string]any {
	return map[string]any{"from":r.From.In(jakartaLocation()).Format(time.RFC3339),"to_exclusive":r.To.In(jakartaLocation()).Format(time.RFC3339),"label":r.label()}
}
