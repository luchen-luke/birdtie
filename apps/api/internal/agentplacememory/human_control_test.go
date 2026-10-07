package agentplacememory

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"strings"
	"testing"
	"time"
)

func humanControlFixture() HumanControl {
	n := time.Now().UTC()
	c := HumanControl{Owner: actorref.PrincipalRef{Type: actorref.Person, ID: "27000000-0000-4000-8000-000000000001"}, AgentID: "27000000-0000-4000-8000-000000000002", PlaceID: "27000000-0000-4000-8000-000000000003", Declarations: []HumanDeclaration{}, ObservedAt: n, ExpiresAt: n.Add(time.Minute), AuthorityStamp: strings.Repeat("a", 64), SourceStamp: strings.Repeat("b", 64)}
	c.SnapshotID = HumanControlSnapshot(c)
	return c
}
func TestPlaceMemoryHumanControlClosedState(t *testing.T) {
	c := humanControlFixture()
	c.Declarations = append(c.Declarations, HumanDeclaration{MemoryID: "27000000-0000-4000-8000-000000000004", Version: 1, Kind: Visited, Basis: SelfDeclaration, Visibility: agentmemory.VisibilityPrivate, Status: agentmemory.StatusExpired, ValidUntil: c.ObservedAt.Add(-time.Second), UpdatedAt: c.ObservedAt.Add(-time.Minute)})
	c.SnapshotID = HumanControlSnapshot(c)
	if ValidateHumanControl(c, c.ObservedAt) != nil {
		t.Fatal("expired human control rejected")
	}
	for _, name := range []string{"attendance", "verified", "visibility", "deleted", "futureExpired", "tamper", "expiry", "nil", "overLimit"} {
		t.Run(name, func(t *testing.T) {
			x := c
			x.Declarations = append([]HumanDeclaration{}, c.Declarations...)
			want := ErrInvalid
			switch name {
			case "attendance":
				x.Declarations[0].Kind = AttendedActivityAt
			case "verified":
				x.Declarations[0].Basis = CurrentBookmark
			case "visibility":
				x.Declarations[0].Visibility = "PUBLIC"
			case "deleted":
				x.Declarations[0].Status = agentmemory.StatusDeleted
			case "futureExpired":
				x.Declarations[0].ValidUntil = x.ObservedAt.Add(time.Hour)
			case "tamper":
				x.SourceStamp = strings.Repeat("c", 64)
			case "expiry":
				want = ErrExpired
			case "nil":
				x.Declarations = nil
			case "overLimit":
				for len(x.Declarations) <= MaxSignals {
					x.Declarations = append(x.Declarations, x.Declarations[0])
				}
			}
			if name != "tamper" {
				x.SnapshotID = HumanControlSnapshot(x)
			}
			now := x.ObservedAt
			if name == "expiry" {
				now = x.ExpiresAt
			}
			if !errors.Is(ValidateHumanControl(x, now), want) {
				t.Fatal("invalid control accepted", name)
			}
		})
	}
	var decoded HumanControl
	if json.Unmarshal([]byte(`{}`), &decoded) != ErrAuthorityJSON {
		t.Fatal("client manufactured control")
	}
}
func TestPlaceMemoryHumanBoundDecodePreservesFiveKeyNativeContract(t *testing.T) {
	n := time.Now().UTC()
	native := `{"expectedVersion":0,"placeId":"27000000-0000-4000-8000-000000000001","kind":"LIKED","visibility":"PRIVATE","validUntil":"` + n.Add(time.Hour).Format(time.RFC3339Nano) + `"}`
	if _, e := DecodePut([]byte(native), n); e != nil {
		t.Fatal("native five-key compatibility", e)
	}
	raw := strings.Replace(native, `{`, `{"agentId":"27000000-0000-4000-8000-000000000002",`, 1)
	if _, _, e := DecodeHumanPut([]byte(raw), n); e != nil {
		t.Fatal(e)
	}
	if _, e := DecodePut([]byte(raw), n); e == nil {
		t.Fatal("human target expanded old native contract")
	}
	for _, v := range []string{native, strings.Replace(raw, `"agentId":`, `"AgentId":`, 1), strings.Replace(raw, `"agentId":"27000000-0000-4000-8000-000000000002"`, `"agentId":null`, 1), strings.Replace(raw, `"agentId":`, `"agentId":"27000000-0000-4000-8000-000000000002","agentId":`, 1), raw + `{}`, strings.Replace(raw, `"agentId":"27000000-0000-4000-8000-000000000002"`, `"agentId":"not-id"`, 1)} {
		if _, _, e := DecodeHumanPut([]byte(v), n); e == nil {
			t.Fatal("invalid human target wire accepted", v)
		}
	}
}
