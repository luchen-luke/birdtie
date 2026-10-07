package agentintroduction

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"strings"
	"testing"
	"time"
)

func TestIntroductionPublicBasisAndClosedHumanResponse(t *testing.T) {
	now := time.Now().UTC()
	a := newpeople.Signal{IntentID: "source", AccountID: "owner", Category: "badminton", Modality: "IN_PERSON", CityID: "city", AreaLabel: "area"}
	b := a
	b.IntentID = "peer"
	b.AccountID = "other"
	for _, community := range []bool{false, true} {
		basis, ok := PublicBasis(a, b, community)
		want := 2
		if community {
			want = 3
		}
		if !ok || len(basis) != want {
			t.Fatal(basis, ok)
		}
		for _, v := range basis {
			if v.Kind == "SHARED_ACTIVITY" {
				t.Fatal("invented activity")
			}
		}
	}
	for _, mode := range []string{"category", "city", "self", "modality", "place"} {
		t.Run(mode, func(t *testing.T) {
			p := b
			switch mode {
			case "category":
				p.Category = "food"
			case "city":
				p.CityID = "elsewhere"
			case "self":
				p.AccountID = a.AccountID
			case "modality":
				p.Modality = "ONLINE"
			case "place":
				p.PlaceID = "one-sided"
			}
			if _, ok := PublicBasis(a, p, true); ok {
				t.Fatal("bypassed existing explicit compatibility")
			}
		})
	}
	out := NewResponse(a.IntentID, now, now.Add(time.Minute))
	raw, e := json.Marshal(out)
	if e != nil || out.Mode != "HUMAN_REVIEW_ONLY" || out.ModelAccess || out.SendAllowed || out.MemoryPromotionAllowed || out.SourceStatus["SHARED_ACTIVITY"] != "UNAVAILABLE" || !strings.Contains(string(raw), "不会发送引荐") {
		t.Fatal(string(raw), e)
	}
}

func TestIntroductionExplicitRegistrationStatusClosed(t *testing.T) {
	now := time.Now().UTC()
	source := "11111111-1111-4111-8111-111111111111"
	peer := "22222222-2222-4222-8222-222222222222"
	account := "33333333-3333-4333-8333-333333333333"
	v := NewResponse(source, now, now.Add(time.Minute))
	v.Candidates = []Candidate{{SourceIntentID: source, CandidateIntentID: peer, AccountID: account, DisplayName: "合成", SourceBinding: strings.Repeat("a", 64), ExpiresAt: v.ExpiresAt, Basis: []Basis{{Kind: "SHARED_INTEREST", Explanation: "公开意图"}, {Kind: "SHARED_ACTIVITY", Explanation: "双方明确公开报名，不代表到场"}}}}
	if ValidateResponse(v, source) == nil {
		t.Fatal("unavailable activity accepted")
	}
	v.SourceStatus["SHARED_ACTIVITY"] = "PUBLIC_REGISTRATIONS_ONLY"
	if ValidateResponse(v, source) != nil {
		t.Fatal(v)
	}
	v.SourceStatus["SHARED_ACTIVITY"] = "ATTENDANCE_VERIFIED"
	if ValidateResponse(v, source) == nil {
		t.Fatal("attendance invented")
	}
}
