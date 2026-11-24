package telegram

import "github.com/raufimusaddiq/richmod/apps/worker/internal/gateway"

const (
	untrustedUserTag     = "untrusted_user_message"
	untrustedLedgerTag   = "untrusted_ledger_text"
	untrustedEvidenceTag = "untrusted_evidence_text"
)

func wrapUntrusted(tag, value string) string {
	return gateway.WrapUntrusted(tag, value)
}

func untrustedUser(value string) string     { return wrapUntrusted(untrustedUserTag, value) }
func untrustedLedger(value string) string   { return wrapUntrusted(untrustedLedgerTag, value) }
func untrustedEvidence(value string) string { return wrapUntrusted(untrustedEvidenceTag, value) }
