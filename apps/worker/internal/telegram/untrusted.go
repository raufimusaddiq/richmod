package telegram

import "regexp"

// Data-boundary tags the agent prompt names. Everything user-, ledger- or
// evidence-controlled is wrapped in one of them so the model reads it as data.
const (
	untrustedUserTag     = "untrusted_user_message"
	untrustedLedgerTag   = "untrusted_ledger_text"
	untrustedEvidenceTag = "untrusted_evidence_text"
)

// untrustedTagPattern matches the start of any boundary tag, opening or closing,
// in any case and with optional inner whitespace.
var untrustedTagPattern = regexp.MustCompile(`(?i)<(\s*/?\s*untrusted_)`)

// wrapUntrusted delimits a controlled string. Any boundary tag already inside the
// value is defanged first, so hostile text cannot close the wrapper early and
// continue as if it were outside it. The wrapper is defense in depth; the
// structural controls (server-built tool catalog, server-resolved refs, Go
// validation of every mutation) are what actually prevent misuse.
func wrapUntrusted(tag, value string) string {
	return "<" + tag + ">" + untrustedTagPattern.ReplaceAllString(value, "&lt;$1") + "</" + tag + ">"
}

func untrustedUser(value string) string     { return wrapUntrusted(untrustedUserTag, value) }
func untrustedEvidence(value string) string { return wrapUntrusted(untrustedEvidenceTag, value) }
