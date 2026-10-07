package onlinesocialopportunity

import (
	"encoding/json"
	"testing"
	"time"
)

func TestOnlineSocialOpportunityClosedProjection(t *testing.T) {
	now := time.Now().UTC()
	id := "11111111-1111-4111-8111-111111111111"
	peer := "22222222-2222-4222-8222-222222222222"
	v := View{SchemaVersion: SchemaVersion, OwnerID: id, IntentID: id, ObservedAt: now, ValidUntil: now.Add(time.Minute), Intents: []Intent{{id, "本人线上需求", now, now.Add(time.Hour)}}, Items: []Item{{ID: id + ":ACTIVITY:" + peer, Title: "合成线上活动", Source: Ref{"ACTIVITY", peer}, Relation: "PUBLIC", SourceVersion: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Hour)}}}
	if e := Validate(v); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*View){func(x *View) { x.Items[0].Source.Type = "PLACE" }, func(x *View) { x.Items[0].ID = peer }, func(x *View) { x.Items[0].TieID = peer }, func(x *View) { x.ValidUntil = now.Add(3 * time.Minute) }, func(x *View) { x.Items = append(x.Items, x.Items[0]) }, func(x *View) { x.Items[0].ExpiresAt = now }, func(x *View) { x.Intents[0].ID = peer }} {
		raw, _ := json.Marshal(v)
		var b View
		json.Unmarshal(raw, &b)
		change(&b)
		if Validate(b) == nil {
			t.Fatal("invalid wire accepted", b)
		}
	}
	if _, e := json.Marshal(Access{}); e == nil {
		t.Fatal("authority serialized")
	}
	if _, e := json.Marshal(Receipt{}); e == nil {
		t.Fatal("proof serialized")
	}
}
