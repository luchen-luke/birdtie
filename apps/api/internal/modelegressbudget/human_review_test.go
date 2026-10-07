package modelegressbudget

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestModelEgressHumanReceiptCannotMarshalAuthority(t *testing.T) {
	r := HumanReceipt{Status: "DRAFT", Preview: &Preview{SourceToken: "PRIVATE_SOURCE_TOKEN", AuthorityToken: "PRIVATE_AUTHORITY", RequestDigest: "PRIVATE_DIGEST"}}
	b, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	for _, v := range []string{"PRIVATE_SOURCE_TOKEN", "PRIVATE_AUTHORITY", "PRIVATE_DIGEST", "Preview"} {
		if strings.Contains(string(b), v) {
			t.Fatal("receipt JSON leaked internal authority", v)
		}
	}
}
