package agentrun

import "testing"

func TestAgentRunRecoveryClosedPolicyAndNoAuthority(t *testing.T) {
	if MaxAttempts != 5 || MaxRootAttempts != 6 || MaxRecoveryGenerations != 1 || MaxRecoveryAttempts != 1 {
		t.Fatal("limits")
	}
	for _, reason := range []string{"INVALID_INPUT", "AUTHORITY_CHANGED", "CANCELLED", "EXPIRED"} {
		if ClassifyReason(reason) != FailurePermanent {
			t.Fatal(reason)
		}
	}
	for _, reason := range []string{"RETRY_BUSY"} {
		if ClassifyReason(reason) != FailureTemporary {
			t.Fatal(reason)
		}
	}
	if ClassifyReason("ATTEMPTS_EXHAUSTED") != FailureExhausted || ClassifyReason("RETRY_UNAVAILABLE") != FailureUnknown || ClassifyReason("RECONCILING") != FailureUnknown || ClassifyReason("RAW_ERROR_PRIVATE") != FailureNone {
		t.Fatal("unknown")
	}
	if ValidateRecoveryInput(RecoveryInput{1, RecoveryReason}) != nil {
		t.Fatal("valid")
	}
	for _, in := range []RecoveryInput{{0, RecoveryReason}, {1, ""}, {1, "confirmed"}, {1, "retry all"}} {
		if ValidateRecoveryInput(in) == nil {
			t.Fatal("open command", in)
		}
	}
}
