package safety

import "testing"

func TestReportTypeAndReasonValidation(t *testing.T) {
	if !ValidTarget("activity", "id") || !ValidTarget("general", "") ||
		!ValidTarget("message", "id") || !ValidTarget("community", "id") || !ValidTarget("business", "id") {
		t.Fatal("expected valid target")
	}
	if ValidTarget("activity", "") || ValidTarget("general", "id") || ValidTarget("memory", "id") {
		t.Fatal("invalid target accepted")
	}
	if !ValidReason("technical") || ValidReason("secret") {
		t.Fatal("reason validation failed")
	}
}
