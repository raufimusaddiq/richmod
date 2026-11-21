package httpx

import (
	"encoding/json"
	"net/http"
)

const MergeTargetCode = "MERGE_TARGET"

func WriteMergeTargetRefusal(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": MergeTargetCode, "error": "Transaksi ini menjadi tujuan penggabungan yang masih aktif. Pisahkan penggabungannya dulu."})
}
