package content

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"testing"
	"time"
)

const copySourceID = "11111111-1111-4111-8111-111111111111"
const copyOperationID = "22222222-2222-4222-8222-222222222222"
const copyPlaceID = "33333333-3333-4333-8333-333333333333"

func copyTestInput() CollectionCopyInput {
	ids := []string{copyPlaceID}
	return CollectionCopyInput{OperationID: copyOperationID, PublicationRevision: 2, GrantVersion: 1, OrderedPlaceIDs: ids, OrderedPlaceIDsSHA256: CollectionCopyPlacesDigest(ids), Title: "我选的地点"}
}
func TestCollectionCopyClosedInputAndIndependentText(t *testing.T) {
	r := CollectionRef{"journey", copySourceID}
	in := copyTestInput()
	raw, _ := json.Marshal(in)
	if _, e := DecodeCollectionCopy(raw, r, false); e != nil {
		t.Fatal(e)
	}
	for _, c := range []struct {
		name   string
		change func(*CollectionCopyInput)
	}{{"duplicate", func(v *CollectionCopyInput) {
		v.OrderedPlaceIDs = append(v.OrderedPlaceIDs, copyPlaceID)
		v.OrderedPlaceIDsSHA256 = CollectionCopyPlacesDigest(v.OrderedPlaceIDs)
	}}, {"digest", func(v *CollectionCopyInput) { v.OrderedPlaceIDsSHA256 = strings.Repeat("0", 64) }}, {"emptyOwnTitle", func(v *CollectionCopyInput) { v.Title = " " }}, {"titleBytes", func(v *CollectionCopyInput) { v.Title = strings.Repeat("我", 54) }}, {"nul", func(v *CollectionCopyInput) { v.Title = "我的\x00地点" }}, {"revision", func(v *CollectionCopyInput) { v.PublicationRevision = CollectionCopyMaxVersion + 1 }}, {"previewApproval", func(v *CollectionCopyInput) { v.Approval = "forged" }}} {
		t.Run(c.name, func(t *testing.T) {
			v := copyTestInput()
			c.change(&v)
			if _, e := NormalizeCollectionCopy(r, v, false); e == nil {
				t.Fatal("invalid copy accepted")
			}
		})
	}
	for _, bad := range []string{strings.TrimSuffix(string(raw), "}") + `,"title":"changed"}`, strings.TrimSuffix(string(raw), "}") + `,"summary":"author text"}`, strings.Replace(string(raw), `"title":"我选的地点"`, `"title":null`, 1), strings.TrimSuffix(string(raw), "}") + `,"coordinates":[57,-2]}`} {
		if _, e := DecodeCollectionCopy([]byte(bad), r, false); e == nil {
			t.Fatal("nonclosed body accepted")
		}
	}
	if !CollectionCopySubsequence([]string{"a", "c"}, []string{"a", "b", "c"}) || CollectionCopySubsequence([]string{"c", "a"}, []string{"a", "b", "c"}) {
		t.Fatal("relative order changed")
	}
}
func TestCollectionCopyPublicationFalseDigestAndGrantPrecision(t *testing.T) {
	entries := []CollectionPublicationEntry{{copyPlaceID, "place"}}
	old := CollectionPublicationDigest("journey", copySourceID, "PUBLISH", 1, entries)
	if old != CollectionPublicationCopyDigest("journey", copySourceID, "PUBLISH", 1, entries, false) || old == CollectionPublicationCopyDigest("journey", copySourceID, "PUBLISH", 1, entries, true) {
		t.Fatal("legacy false digest changed or true not bound")
	}
	for _, tail := range []string{"", `,"allowPrivatePlaceCopy":false`, `,"allowPrivatePlaceCopy":true`} {
		raw := `{"operationId":"` + copyOperationID + `","revision":1,"entries":[{"placeId":"` + copyPlaceID + `","locationVisibility":"place"}]` + tail + `}`
		if _, e := DecodeCollectionPublication([]byte(raw)); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := DecodeCollectionPublication([]byte(`{"operationId":"` + copyOperationID + `","revision":1,"entries":[],"allowPrivatePlaceCopy":true,"allowPrivatePlaceCopy":false}`)); e == nil {
		t.Fatal("duplicate consent accepted")
	}
	now := time.Now().UTC()
	v := PublicCollection{ID: copySourceID, Kind: "journey", Revision: 2, SourceExpiresAt: now.Add(time.Minute), Entries: []PublicCollectionEntry{{LocationVisibility: "city"}, {LocationVisibility: "place", PlaceID: ptrCopyID(copyPlaceID)}}}
	g := CollectionCopyGrant{SourceRef: CollectionRef{v.Kind, v.ID}, PublicationRevision: 2, GrantVersion: 1, EligiblePlaceIDs: []string{copyPlaceID}, OrderedPlaceIDsSHA256: CollectionCopyPlacesDigest([]string{copyPlaceID}), ExpiresAt: v.SourceExpiresAt}
	if ValidateCollectionCopyGrant(g, v) != nil {
		t.Fatal("public-only grant rejected")
	}
	g.EligiblePlaceIDs = append([]string{copySourceID}, g.EligiblePlaceIDs...)
	g.OrderedPlaceIDsSHA256 = CollectionCopyPlacesDigest(g.EligiblePlaceIDs)
	if ValidateCollectionCopyGrant(g, v) == nil {
		t.Fatal("hidden or city ID escaped")
	}
}
func ptrCopyID(s string) *string { return &s }
func TestCollectionCopyMinimalReceiptAndOriginalSession(t *testing.T) {
	raw, _ := json.Marshal(CollectionCopyReceipt{OperationID: copyOperationID, Status: "STOPPED"})
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(raw, &keys)
	if len(keys) != 4 || string(keys["destinationRef"]) != "null" || strings.Contains(string(raw), "sourceRef") {
		t.Fatal(string(raw))
	}
	d := [32]byte{1}
	a := identity.Actor{ID: copySourceID, AccountType: "person"}
	f := CollectionSessionFrame{ID: copyOperationID, Generation: 1}
	ctx := WithCollectionSession(context.Background(), d, a, f)
	if _, valid, _ := OriginalCollectionSession(ctx, d, a); !valid {
		t.Fatal("original frame absent")
	}
	ctx = WithCollectionSession(ctx, d, a, CollectionSessionFrame{ID: f.ID, Generation: 2})
	if _, valid, _ := OriginalCollectionSession(ctx, d, a); valid {
		t.Fatal("fresh frame replaced original")
	}
}
func TestCollectionCopyNestedPublicationAndReadOnlyRestriction(t *testing.T) {
	for _, entry := range []string{`{"placeId":"` + copyPlaceID + `","locationVisibility":"place","note":"author"}`, `{"placeId":"` + copyPlaceID + `","placeId":"` + copySourceID + `","locationVisibility":"place"}`, `{"placeId":"` + copyPlaceID + `","locationVisibility":null}`} {
		raw := `{"operationId":"` + copyOperationID + `","revision":1,"entries":[` + entry + `]}`
		if _, e := DecodeCollectionPublication([]byte(raw)); e == nil {
			t.Fatal("nonclosed nested publisher selector")
		}
	}
	b, _ := json.Marshal(PersonalCollection{PrivatePlaceCopyRestricted: true})
	if !strings.Contains(string(b), `"privatePlaceCopyRestricted":true`) {
		t.Fatal("read capability missing")
	}
	b, _ = json.Marshal(PersonalCollection{})
	if strings.Contains(string(b), "privatePlaceCopyRestricted") {
		t.Fatal("legacy omission changed")
	}
	in := copyTestInput()
	r := CollectionRef{"journey", copySourceID}
	b, _ = json.Marshal(in)
	b = []byte(strings.TrimSuffix(string(b), "}") + `,"privatePlaceCopyRestricted":false}`)
	if _, e := DecodeCollectionCopy(b, r, false); e == nil {
		t.Fatal("read-only capability became command input")
	}
}
