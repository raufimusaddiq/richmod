package telegram

import "testing"

func TestDisputeStatusesHaveSpecificRepliesNotGenericSuccess(t *testing.T) {
	generic := agentMutationFallback(agentToolResult{Status: "UNKNOWN_STATUS_FOR_TEST"})
	seen := map[string]string{}
	for _, status := range []string{
		"EVIDENCE_DISPUTE_STAGED", "EVIDENCE_DISPUTE_ALREADY_STAGED", "EVIDENCE_DISPUTE_INVALID",
		"EVIDENCE_DISPUTE_UNAUTHORIZED",
	} {
		reply := agentMutationFallback(agentToolResult{Tool: "dispute_evidence_link", Status: status})
		if reply == "" || reply == generic {
			t.Fatalf("%s reply = %q, want a specific message", status, reply)
		}
		if other, duplicate := seen[reply]; duplicate {
			t.Fatalf("%s and %s share the reply %q", status, other, reply)
		}
		seen[reply] = status
	}
}
