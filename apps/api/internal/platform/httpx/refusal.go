// Package httpx holds JSON refusals that more than one API package must answer identically.
package httpx

import (
	"encoding/json"
	"net/http"
)

// MergeTargetCode is the machine-readable code of the refusal below; the web client keys on it.
const MergeTargetCode = "MERGE_TARGET"

// WriteMergeTargetRefusal writes the 409 for voiding, rejecting or ignoring a transaction that is
// the target of an ACTIVE reconciliation merge (reviewdomain.ErrMergeTargetActive). The household
// resolves it with "Pisahkan penggabungan" in the transaction drawer.
func WriteMergeTargetRefusal(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": MergeTargetCode, "error": "Transaksi ini menjadi tujuan penggabungan yang masih aktif. Pisahkan penggabungannya dulu."})
}
