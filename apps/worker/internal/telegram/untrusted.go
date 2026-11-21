package telegram

import "regexp"

const (
	untrustedUserTag     = "untrusted_user_message"
	untrustedLedgerTag   = "untrusted_ledger_text"
	untrustedEvidenceTag = "untrusted_evidence_text"
)

var untrustedTagPattern = regexp.MustCompile(`(?i)<(\s*/?\s*untrusted_)`)

func wrapUntrusted(tag, value string) string {
	return "<" + tag + ">" + untrustedTagPattern.ReplaceAllString(value, "&lt;$1") + "</" + tag + ">"
}

func untrustedUser(value string) string     { return wrapUntrusted(untrustedUserTag, value) }
func untrustedLedger(value string) string   { return wrapUntrusted(untrustedLedgerTag, value) }
func untrustedEvidence(value string) string { return wrapUntrusted(untrustedEvidenceTag, value) }
