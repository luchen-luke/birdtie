package contextgraph

import (
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"testing"
)

func TestDeclarationControlCitySlugAndClosedRemoval(t *testing.T) {
	op := "22222222-2222-2222-2222-222222222222"
	body := `{"operationId":"` + op + `","action":"DECLARE","expectedVersion":0,"contextType":"CITY","sourceKey":"aberdeen-gb","relation":"current"}`
	v, e := DecodeDeclarationControl([]byte(body), false)
	if e != nil || v.SourceKey != "aberdeen-gb" {
		t.Fatal(v, e)
	}
	digest := DeclarationControlDigest(v)
	v.SourceKey = "other-city"
	if digest == DeclarationControlDigest(v) {
		t.Fatal("city not bound")
	}
	for n, b := range map[string]string{"missingVersion": strings.Replace(body, `,"expectedVersion":0`, "", 1), "nullVersion": strings.Replace(body, `"expectedVersion":0`, `"expectedVersion":null`, 1), "borrowedRole": strings.Replace(body, `"relation":"current"`, `"relation":"current","role":"member"`, 1), "fakeID": strings.Replace(body, `"contextType":"CITY"`, `"contextId":"`+op+`","contextType":"CITY"`, 1)} {
		t.Run(n, func(t *testing.T) {
			if _, e := DecodeDeclarationControl([]byte(b), false); e == nil {
				t.Fatal("unsafe declaration accepted")
			}
		})
	}
	v = DeclarationControlInput{OperationID: op, Action: "REMOVE", ContextID: op, ExpectedVersion: identity.HumanControlMaxVersion, Relation: "current"}
	if _, e := NormalizeDeclarationControl(v, false); e != nil {
		t.Fatal("terminal remove", e)
	}
	v.Action = "DECLARE"
	v.ContextID = ""
	v.Type = City
	v.SourceKey = "aberdeen-gb"
	if _, e := NormalizeDeclarationControl(v, false); e == nil {
		t.Fatal("nonterminal overflow")
	}
}
