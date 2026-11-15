package telegram

import "regexp"

// Data-boundary tags the agent prompt names; user-, ledger- and evidence-controlled text is wrapped in one so
// the model reads it as data.
const (
	untrustedUserTag     = "untrusted_user_message"
	untrustedLedgerTag   = "untrusted_ledger_text"
	untrustedEvidenceTag = "untrusted_evidence_text"
)

// untrustedTagPattern matches any boundary tag, opening or closing, in any case, with optional inner whitespace.
var untrustedTagPattern = regexp.MustCompile(`(?i)<(\s*/?\s*untrusted_)`)

// wrapUntrusted defangs boundary tags already in the value so hostile text cannot close the wrapper early; it
// is defense in depth beside the server-built tool catalog, resolved refs and Go validation.
func wrapUntrusted(tag, value string) string {
	return "<" + tag + ">" + untrustedTagPattern.ReplaceAllString(value, "&lt;$1") + "</" + tag + ">"
}

func untrustedUser(value string) string     { return wrapUntrusted(untrustedUserTag, value) }
func untrustedLedger(value string) string   { return wrapUntrusted(untrustedLedgerTag, value) }
func untrustedEvidence(value string) string { return wrapUntrusted(untrustedEvidenceTag, value) }
