package agentworkspace

import (
	"strings"
	"testing"
)

func TestTaskPrincipalRefContract(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	for _, kind := range []string{"person", "organization", "business", "community"} {
		ref, err := (Task{PrincipalType: kind, PrincipalID: id}).PrincipalRef()
		if err != nil || string(ref.Type) != strings.ToUpper(kind) || ref.ID != id {
			t.Fatalf("%s: ref=%+v err=%v", kind, ref, err)
		}
	}
	if _, err := (Task{PrincipalType: "CITY", PrincipalID: id}).PrincipalRef(); err == nil {
		t.Fatal("city context must not become a principal")
	}
}
