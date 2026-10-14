package contactcode

import (
	"testing"
	"time"
)

func TestContactCodeRejectsInvalidOperationAndInconsistentProjection(t *testing.T) {
	for _, in := range []Input{{}, {OperationID: "owner"}, {OperationID: "00000000-0000-0000-0000-000000000000"}, {OperationID: "11111111-1111-4111-8111-111111111111", ExpectedVersion: -1}} {
		if ValidateInput(in) == nil {
			t.Fatal("invalid change accepted", in)
		}
	}
	id := "11111111-1111-4111-8111-111111111111"
	at := time.Now().UTC()
	for _, v := range []View{{Version: -1}, {CodeID: &id}, {ExpiresAt: &at}} {
		if ValidView(v) {
			t.Fatal("inconsistent projection accepted")
		}
	}
	if !ValidView(View{}) || !ValidView(View{Version: 1, CodeID: &id, ExpiresAt: &at}) {
		t.Fatal("valid projection rejected")
	}
}
