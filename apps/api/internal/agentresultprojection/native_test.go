package agentresultprojection

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"testing"
	"time"
)

func TestAgentResultProjectionClosedNativeQueryAndReceipt(t *testing.T) {
	q := Query{CityID: "native-city", Kind: "opportunity", TimePreference: "anytime", CompareIDs: []string{}}
	if !q.Valid() {
		t.Fatal("closed opportunity read")
	}
	for _, bad := range []Query{{CityID: q.CityID, Kind: "memory"}, {CityID: q.CityID, Kind: "none"}, {CityID: q.CityID, Kind: q.Kind, Closer: true}, {CityID: q.CityID, Kind: q.Kind, SearchTerm: "ignored search"}, {CityID: q.CityID, Kind: q.Kind, Category: "badminton"}, {CityID: q.CityID, Kind: q.Kind, TimePreference: "tomorrow"}, {CityID: q.CityID, Kind: q.Kind, Bounds: &Bounds{West: -1, South: -1, East: 1, North: 1}}} {
		if bad.Valid() {
			t.Fatal("unsupported query silently accepted", bad)
		}
	}
	a := Access{Actor: identity.Actor{ID: "owner", AccountType: "person"}, SessionDigest: [32]byte{1}, TaskID: "task", ExpectedTask: json.RawMessage(`{"id":"task"}`)}
	if !a.Valid() {
		t.Fatal("own request binding")
	}
	a.Actor.AccountType = "organization"
	if a.Valid() {
		t.Fatal("organization borrowed person projection")
	}
	now := time.Now().UTC()
	ref := Ref{Type: "activity", ID: "original-id"}
	r := Receipt{Items: []Item{{Entity: ref, Title: "合成原对象", Scope: AuthorizedView, Detail: &ref, Share: &ref}}, PublicCommercialRefs: []Ref{ref}, ObservedAt: now, ValidUntil: now.Add(time.Minute), Proof: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Seal: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	if !r.Valid() {
		t.Fatal("native receipt shape")
	}
	r.PublicCommercialRefs = append(r.PublicCommercialRefs, ref)
	if r.Valid() {
		t.Fatal("duplicate commercial ref")
	}
	r.PublicCommercialRefs = []Ref{{Type: "person", ID: "owner"}}
	if r.Valid() {
		t.Fatal("person promoted")
	}
	r.PublicCommercialRefs = []Ref{ref}
	r.Items[0].Scope = SelfPrivate
	if r.Valid() {
		t.Fatal("private opportunity promoted")
	}
}
