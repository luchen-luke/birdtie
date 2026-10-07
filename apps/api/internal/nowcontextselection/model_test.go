package nowcontextselection

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNowSelectionClosedInputAndServerOnly(t *testing.T) {
	good := `{"optionsToken":"` + strings.Repeat("a", 90) + `","optionId":"` + strings.Repeat("a", 64) + `"}`
	if _, e := Decode([]byte(good)); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`null`, good + ` {}`, strings.Replace(good, `"optionId":`, `"confirmed":true,"optionId":`, 1), strings.Replace(good, `"optionId":`, `"optionId":"dup","optionId":`, 1)} {
		if _, e := Decode([]byte(raw)); !errors.Is(e, ErrInvalid) {
			t.Fatal("accepted bad input", e)
		}
	}
	for _, v := range []any{Access{}, OptionsReceipt{}, SelectionReceipt{}} {
		if _, e := json.Marshal(v); !errors.Is(e, ErrDenied) {
			t.Fatal("server receipt serialized", e)
		}
	}
}
func TestNowSelectionPreservesTypedSourceAndDeclaredSemantics(t *testing.T) {
	o := Option{OptionID: strings.Repeat("a", 64), ContextID: "00000000-0000-4000-8000-000000000001", ContextType: "CITY", CityID: "aberdeen-gb", Label: "公开城市", ViewMode: "DESTINATION", QueryRoute: "CITY"}
	if ValidateOption(o) != nil {
		t.Fatal(o)
	}
	o.Declared = true
	if ValidateOption(o) == nil {
		t.Fatal("invented destination declaration")
	}
	o.Relation = "past"
	o.ViewMode = "PAST"
	if ValidateOption(o) != nil {
		t.Fatal(o)
	}
	o.ContextType = "INSTITUTION"
	o.CityID = ""
	o.QueryRoute = "UNAVAILABLE"
	if ValidateOption(o) != nil {
		t.Fatal(o)
	}
	o.QueryRoute = "CITY"
	if ValidateOption(o) == nil {
		t.Fatal("institution relabeled City")
	}
}
