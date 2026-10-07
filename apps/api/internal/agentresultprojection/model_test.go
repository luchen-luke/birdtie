package agentresultprojection

import (
	"math"
	"testing"
)

func TestSevenOriginalRefsDriveAllConsumers(t *testing.T) {
	items := []Item{}
	for _, k := range []string{"person", "activity", "place", "community", "organization", "business"} {
		ref := Ref{Type: k, ID: "original-" + k}
		items = append(items, Item{Entity: ref, Title: "原实体", Scope: AuthorizedView, Detail: &ref, Share: &ref})
	}
	activity := Ref{Type: "activity", ID: "original-activity"}
	items = append(items, Item{Entity: Ref{Type: "opportunity", ID: "own-intent:original-activity"}, Title: "原活动", Scope: SelfPrivate, Detail: &activity, Share: &activity})
	if err := ValidateItems(items); err != nil {
		t.Fatal(err)
	}
	refs := Refs(items)
	for n, item := range items {
		if refs[n] != item.Entity {
			t.Fatal("consumer changed original entity ref")
		}
	}
	if len(PinIDs(items)) != 0 {
		t.Fatal("no coordinate must not create a pin")
	}
	if *items[6].Share != activity || *items[6].Detail != activity {
		t.Fatal("private candidate became share target")
	}
}

func TestRejectInvalidOrEscalatedProjection(t *testing.T) {
	r := Ref{Type: "activity", ID: "activity-a"}
	good := Item{Entity: r, Title: "活动", Scope: AuthorizedView, Detail: &r, Share: &r}
	mutations := []func(*Item){
		func(i *Item) { i.Entity.Type = "unknown" },
		func(i *Item) { i.Entity.ID = "" },
		func(i *Item) { i.Title = "\x00" },
		func(i *Item) { i.Scope = SelfPrivate },
		func(i *Item) { i.Detail = &Ref{Type: "business", ID: r.ID} },
		func(i *Item) { i.Share = &Ref{Type: "activity", ID: "other"} },
		func(i *Item) { i.Anchor = &Anchor{CoordinateSystem: "wgs84", Precision: "point", Latitude: math.NaN()} },
		func(i *Item) { i.Anchor = &Anchor{CoordinateSystem: "wgs84", Precision: "point", Latitude: 91} },
		func(i *Item) { i.Anchor = &Anchor{CoordinateSystem: "bd09", Precision: "point"} },
	}
	for n, mutate := range mutations {
		item := good
		mutate(&item)
		if ValidateItem(item) == nil {
			t.Fatalf("mutation %d accepted", n)
		}
	}
	if ValidateItems([]Item{good, good}) == nil || ValidateItems(nil) == nil {
		t.Fatal("ambiguous or absent projection accepted")
	}
}

func TestPrivateOpportunityNeverSharesIntent(t *testing.T) {
	activity := Ref{Type: "activity", ID: "activity-a"}
	intent := Ref{Type: "opportunity", ID: "intent-a:activity-a"}
	i := Item{Entity: intent, Title: "活动", Scope: SelfPrivate, Detail: &activity, Share: &activity}
	if ValidateItem(i) != nil {
		t.Fatal("original candidate rejected")
	}
	i.Share = &intent
	if ValidateItem(i) == nil {
		t.Fatal("private intent candidate leaked")
	}
	i.Share = &activity
	i.Scope = AuthorizedView
	if ValidateItem(i) == nil {
		t.Fatal("private candidate became public")
	}
	i.Scope = SelfPrivate
	i.Detail = &Ref{Type: "activity", ID: "other"}
	if ValidateItem(i) == nil {
		t.Fatal("wrong activity opened")
	}
}

func TestPersonRequiresExplicitBroadAreaAndLegacyGroupIsNotCommunity(t *testing.T) {
	if ValidAnchor("person", &Anchor{CoordinateSystem: "wgs84", Precision: "point", PublicZone: "north"}) {
		t.Fatal("exact person pin")
	}
	if ValidAnchor("person", &Anchor{CoordinateSystem: "wgs84", Precision: "area", PublicZone: "street"}) {
		t.Fatal("unapproved public area")
	}
	if !ValidAnchor("person", &Anchor{CoordinateSystem: "wgs84", Precision: "area", PublicZone: "north"}) {
		t.Fatal("explicit coarse opt-in lost")
	}
	group := Item{Entity: Ref{Type: "group", ID: "legacy"}, Title: "旧社群", Scope: AuthorizedView}
	if ValidateItem(group) != nil || ProductKind(group.Entity.Type) {
		t.Fatal("legacy Group confused with Community")
	}
	group.Detail = &Ref{Type: "community", ID: "legacy"}
	if ValidateItem(group) == nil {
		t.Fatal("renamed legacy source")
	}
}
