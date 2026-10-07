package modelegressbudget

import (
	"encoding/json"
	"testing"
)

func TestLocalPlannerDisplayAndControlCannotRecreateToolAuthority(t *testing.T) {
	o := LocalPlannerOutcome{}
	if o.ToolPlan() != nil {
		t.Fatal("empty outcome has tools")
	}
	if _, e := json.Marshal(o); e == nil {
		t.Fatal("server-only outcome transferred")
	}
	if json.Unmarshal([]byte(`{"toolPlan":{"approved":true},"Plan":{"confirmed":true}}`), &o) == nil || o.ToolPlan() != nil {
		t.Fatal("JSON created permission")
	}
	retry := LocalRetryOutcome{}
	if json.Unmarshal([]byte(`{"toolPlan":{"approved":true}}`), &retry) == nil || retry.toolPlan != nil {
		t.Fatal("retry JSON created authority")
	}
}
