package agenttool

import "testing"

func TestSandboxRegistryPersistentNativeMetadataStillRequiresExactConfirmation(t *testing.T) {
	d, ok := Lookup(SandboxWrite)
	if !ok || d.Version != "sandbox.write.v1" || d.Purpose != "PREPARE_OWN_SANDBOX_APPROVAL" || d.Idempotency != "NATIVE_OWNER_LOGICAL_OPERATION_ACTION_EFFECT" || d.Reconciliation != "OWN_NATIVE_SANDBOX_FENCE_AND_EFFECT_ROW" {
		t.Fatal("native sandbox metadata", d)
	}
	for _, tool := range []string{"message.send", "activity.join", "booking.create", "shell.execute", "profile.update"} {
		if _, ok := Lookup(tool); ok {
			t.Fatal("real tool activated", tool)
		}
	}
}
