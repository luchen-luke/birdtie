package agentresultprojection

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aca "github.com/birdtie/birdtie/apps/api/internal/agentcontextadapter"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const publicEvidenceUnitOwner = "52000000-0000-4000-8000-000000000001"
const publicEvidenceUnitTask = "52000000-0000-4000-8000-000000000002"

func publicEvidenceUnitInput(kind string, n int) (Access, Query, Receipt) {
	now := time.Date(2026, 10, 7, 4, 0, 0, 123000000, time.UTC)
	task := json.RawMessage(`{"id":"` + publicEvidenceUnitTask + `","principalType":"person","principalId":"` + publicEvidenceUnitOwner + `","query":"原任务","updatedAt":"2026-10-07T03:00:00Z"}`)
	a := Access{Actor: identity.Actor{ID: publicEvidenceUnitOwner, AccountType: "person"}, SessionDigest: [32]byte{1}, TaskID: publicEvidenceUnitTask, ExpectedTask: task}
	r := Receipt{ObservedAt: now, ValidUntil: now.Add(20 * time.Second), Proof: strings.Repeat("a", 64), Seal: strings.Repeat("b", 64), Items: []Item{}, PublicCommercialRefs: []Ref{}, Activities: []foundation.Activity{}, Places: []foundation.Place{}}
	for i := 0; i < n; i++ {
		ref := Ref{Type: kind, ID: fmt.Sprintf("52000000-0000-4000-8000-%012d", i+3)}
		r.Items = append(r.Items, Item{Entity: ref, Scope: AuthorizedView, Title: "获准公开原对象", Detail: &ref})
		r.PublicCommercialRefs = append(r.PublicCommercialRefs, ref)
		expires := now.Add(24 * time.Hour)
		source := foundation.Source{UpdatedAt: now.Add(-time.Hour), ExpiresAt: &expires}
		if kind == "activity" {
			r.Activities = append(r.Activities, foundation.Activity{ID: ref.ID, CityID: "aberdeen-gb", Visibility: "public", Title: "获准公开原对象", CategoryCode: "badminton", StartsAt: now.Add(time.Hour), EndsAt: now.Add(2 * time.Hour), Organizer: foundation.ActivityOrganizer{Type: "person", ID: publicEvidenceUnitOwner}, Source: source})
		} else {
			r.Places = append(r.Places, foundation.Place{ID: ref.ID, CityID: "aberdeen-gb", Name: "原地点", CategoryCode: "sports", Source: source})
		}
	}
	return a, Query{CityID: "aberdeen-gb", Kind: kind}, r
}
func TestPublicFieldEvidenceUnitOriginalSixAndTwoFields(t *testing.T) {
	for _, kind := range []string{"activity", "place"} {
		t.Run(kind, func(t *testing.T) {
			a, q, r := publicEvidenceUnitInput(kind, 1)
			before, _ := json.Marshal(r.Activities)
			placeBefore, _ := json.Marshal(r.Places)
			p, e := BuildPublicFieldEvidence(a, q, r)
			want := 2
			if kind == "activity" {
				want = 6
			}
			if e != nil || p.Status != "AVAILABLE" || len(p.FieldEvidenceSet.Claims) != want || acb.ValidateFieldEvidenceShape(*p.FieldEvidenceSet) != nil {
				t.Fatalf("invalid current public claims: %v %+v", e, p)
			}
			if p.GrantsAuthority || p.ModelAccess != "UNAVAILABLE" || p.MediaAccess != "UNAVAILABLE" || len(p.FieldEvidenceSet.Conflicts) != 0 || !p.ObservedAt.Equal(r.ObservedAt) || !p.ValidUntil.Equal(r.ValidUntil) {
				t.Fatal("invented authority/conflict/read clock")
			}
			for _, c := range p.FieldEvidenceSet.Claims {
				if c.ClaimantKind != "NATIVE_DOMAIN_RECORD" || c.ClaimantID != "" || c.DeclaredAt != nil || c.CapturedAt != nil || c.SourceCreatedAt != nil || c.ValidFrom != nil || c.Confidence != nil || c.Source.RowToken != "" || c.CollectionEvent != "CURRENT_NATIVE_READ" || c.CaptureTimeStatus != "UNKNOWN_NOT_COLLECTED" || !c.SourceUpdatedAt.Equal(r.ObservedAt.Add(-time.Hour)) || c.ValidUntil == nil || !c.ValidUntil.Equal(r.ObservedAt.Add(24*time.Hour)) {
					t.Fatalf("invented time/declarator/score: %+v", c)
				}
				if kind == "activity" && c.Use != "SCHEDULE_OR_RECORD_NOT_ATTENDANCE" {
					t.Fatal("schedule became attendance")
				}
			}
			after, _ := json.Marshal(r.Activities)
			placeAfter, _ := json.Marshal(r.Places)
			if string(before) != string(after) || string(placeBefore) != string(placeAfter) {
				t.Fatal("source rows mutated")
			}
			raw, _ := json.Marshal(p)
			if p.Budget.Unit != aca.BudgetUnit || p.Budget.Limit != 16384 || p.Budget.Used != len(raw) || strings.Contains(string(raw), r.Proof) || strings.Contains(string(raw), "获准公开原对象") {
				t.Fatal("wrapper budget or private/field values leaked")
			}
		})
	}
}
func TestPublicFieldEvidenceUnitPublicIntersectionPreservesDomain(t *testing.T) {
	for _, mode := range []string{"invite_only", "member_only", "missing_public_ref", "wrong_row", "duplicate_row", "no_row", "unauthorized_scope"} {
		t.Run(mode, func(t *testing.T) {
			a, q, r := publicEvidenceUnitInput("activity", 1)
			switch mode {
			case "invite_only", "member_only":
				r.Activities[0].Visibility = mode
			case "missing_public_ref":
				r.PublicCommercialRefs = []Ref{}
			case "wrong_row":
				r.Activities[0].ID = publicEvidenceUnitOwner
			case "duplicate_row":
				r.Activities = append(r.Activities, r.Activities[0])
			case "no_row":
				r.Activities = nil
			case "unauthorized_scope":
				r.Items[0].Scope = SelfPrivate
				r.PublicCommercialRefs = nil
			}
			items, _ := json.Marshal(r.Items)
			p, e := BuildPublicFieldEvidence(a, q, r)
			if mode == "unauthorized_scope" {
				// An activity SELF_PRIVATE item is invalid in the existing typed
				// contract. Required receipt errors must refuse, not look empty.
				if e != ErrInvalid || p != nil {
					t.Fatal("invalid activity scope became optional metadata")
				}
				return
			}
			if e != nil || len(p.FieldEvidenceSet.Claims) != 0 {
				t.Fatalf("authorized/private result mislabeled PUBLIC %v %+v", e, p)
			}
			after, _ := json.Marshal(r.Items)
			if string(items) != string(after) {
				t.Fatal("deleted legal original result")
			}
		})
	}
}
func TestPublicFieldEvidenceUnitUnknownAndExpiredSourceAreDescriptiveOnly(t *testing.T) {
	for _, mode := range []string{"zero_updated", "future_updated", "expired_source", "invalid_expiry", "unknown_expiry", "source_lease_shorter"} {
		t.Run(mode, func(t *testing.T) {
			a, q, r := publicEvidenceUnitInput("activity", 1)
			source := &r.Activities[0].Source
			switch mode {
			case "zero_updated":
				source.UpdatedAt = time.Time{}
			case "future_updated":
				source.UpdatedAt = r.ObservedAt.Add(time.Second)
			case "expired_source":
				v := r.ObservedAt.Add(-time.Second)
				source.ExpiresAt = &v
			case "invalid_expiry":
				v := time.Time{}
				source.ExpiresAt = &v
			case "unknown_expiry":
				source.ExpiresAt = nil
			case "source_lease_shorter":
				v := r.ObservedAt.Add(time.Second)
				source.ExpiresAt = &v
			}
			p, e := BuildPublicFieldEvidence(a, q, r)
			if e != nil || len(r.Activities) != 1 || len(r.Items) != 1 || !p.ValidUntil.Equal(r.ValidUntil) {
				t.Fatalf("optional metadata altered original access %v", e)
			}
			if mode == "unknown_expiry" {
				if len(p.FieldEvidenceSet.Claims) != 6 || p.FieldEvidenceSet.Claims[0].ValidUntil != nil || p.FieldEvidenceSet.Claims[0].ValidityStatus != "SOURCE_INTERVAL_UNKNOWN" {
					t.Fatal("unknown expiry renewed/inferred")
				}
			} else if mode == "source_lease_shorter" {
				if len(p.FieldEvidenceSet.Claims) != 6 || !p.FieldEvidenceSet.Claims[0].ValidUntil.Equal(*source.ExpiresAt) {
					t.Fatal("source expiry conflated with read lease")
				}
			} else if p.Status != "UNAVAILABLE" || len(p.FieldEvidenceSet.Claims) != 0 {
				t.Fatal("bad/expired source fabricated evidence")
			}
		})
	}
}
func TestPublicFieldEvidenceUnitBoundedWrapperWholeEntityGroups(t *testing.T) {
	for _, tc := range []struct {
		kind      string
		n, fields int
	}{{"place", 100, 2}, {"activity", 30, 6}} {
		t.Run(tc.kind, func(t *testing.T) {
			a, q, r := publicEvidenceUnitInput(tc.kind, tc.n)
			p, e := BuildPublicFieldEvidence(a, q, r)
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(p)
			claims := len(p.FieldEvidenceSet.Claims)
			if len(raw) > 16384 || claims > 100 || claims%tc.fields != 0 || p.Status != "PARTIAL" || p.Budget.Used != len(raw) || p.Budget.Omitted["OMITTED_BUDGET"] != tc.n-claims/tc.fields {
				t.Fatalf("unbounded/split metadata groups %d %d %+v", len(raw), claims, p.Budget)
			}
			if len(r.Items) != tc.n || (tc.kind == "place" && len(r.Places) != 100) || (tc.kind == "activity" && len(r.Activities) != 30) {
				t.Fatal("original source limits weakened")
			}
			counts := map[string]int{}
			for _, c := range p.FieldEvidenceSet.Claims {
				counts[c.ItemID]++
			}
			for _, n := range counts {
				if n != tc.fields {
					t.Fatal("split entity group")
				}
			}
			if acb.ValidateFieldEvidenceShape(*p.FieldEvidenceSet) != nil {
				t.Fatal("invalid budgeted field set")
			}
		})
	}
}
func TestPublicFieldEvidenceUnitBindingCannotMoveOrTrustWire(t *testing.T) {
	a, q, r := publicEvidenceUnitInput("activity", 1)
	p, e := BuildPublicFieldEvidence(a, q, r)
	if e != nil {
		t.Fatal(e)
	}
	owner, _ := actorref.ParsePrincipal("PERSON", a.Actor.ID)
	copy := p.ForResponse(owner, a.TaskID, a.ExpectedTask, r.Items, r.PublicCommercialRefs)
	if copy == nil {
		t.Fatal("exact native binding failed")
	}
	copy.FieldEvidenceSet.Claims[0].Field = "tampered"
	if p.FieldEvidenceSet.Claims[0].Field == "tampered" {
		t.Fatal("metadata aliases returned view")
	}
	for _, mode := range []string{"owner", "task", "version", "refs", "public_refs", "content", "wire"} {
		t.Run(mode, func(t *testing.T) {
			o, id, task, items, refs := owner, a.TaskID, a.ExpectedTask, append([]Item{}, r.Items...), append([]Ref{}, r.PublicCommercialRefs...)
			current := p
			switch mode {
			case "owner":
				o.ID = publicEvidenceUnitTask
			case "task":
				id = publicEvidenceUnitOwner
			case "version":
				task = json.RawMessage(strings.Replace(string(task), "03:00:00Z", "03:00:01Z", 1))
			case "refs":
				items[0].Title = "new title"
			case "public_refs":
				refs = nil
			case "content":
				current = copy
			case "wire":
				raw, _ := json.Marshal(p)
				current = &PublicFieldEvidence{}
				json.Unmarshal(raw, current)
			}
			if current.ForResponse(o, id, task, items, refs) != nil {
				t.Fatal("old metadata moved or wire assertion trusted")
			}
		})
	}
}
func TestPublicFieldEvidenceUnitRequiredAuthorityErrorsNotOptional(t *testing.T) {
	for _, mode := range []string{"actor", "digest", "task_owner", "task_id", "receipt", "query"} {
		t.Run(mode, func(t *testing.T) {
			a, q, r := publicEvidenceUnitInput("activity", 1)
			switch mode {
			case "actor":
				a.Actor.AccountType = "organization"
			case "digest":
				a.SessionDigest = [32]byte{}
			case "task_owner":
				a.ExpectedTask = json.RawMessage(strings.Replace(string(a.ExpectedTask), publicEvidenceUnitOwner, publicEvidenceUnitTask, 1))
			case "task_id":
				a.TaskID = publicEvidenceUnitOwner
			case "receipt":
				r.Proof = ""
			case "query":
				q.CityID = ""
			}
			if p, e := BuildPublicFieldEvidence(a, q, r); e == nil || p != nil {
				t.Fatal("required authority failure downgraded to optional metadata")
			}
		})
	}
	for _, kind := range []string{"person", "organization", "community", "business", "opportunity"} {
		a, q, r := publicEvidenceUnitInput("activity", 1)
		q.Kind = kind
		if p, e := BuildPublicFieldEvidence(a, q, r); e != nil || p != nil {
			t.Fatal("unsupported kind got public metadata")
		}
	}
}
func TestPublicFieldEvidenceUnitEmptyAndCurrentProofFingerprint(t *testing.T) {
	a, q, r := publicEvidenceUnitInput("place", 0)
	p, e := BuildPublicFieldEvidence(a, q, r)
	if e != nil || p.Status != "NO_PUBLIC_FIELDS" || len(p.FieldEvidenceSet.Claims) != 0 {
		t.Fatal("empty turned into fabricated evidence")
	}
	a, q, r = publicEvidenceUnitInput("activity", 1)
	p, e = BuildPublicFieldEvidence(a, q, r)
	if e != nil {
		t.Fatal(e)
	}
	r.Proof = strings.Repeat("c", 64)
	next, e := BuildPublicFieldEvidence(a, q, r)
	if e != nil {
		t.Fatal(e)
	}
	if p.FieldEvidenceSet.Claims[0].Source.Version == next.FieldEvidenceSet.Claims[0].Source.Version {
		t.Fatal("opaque source closure version ignored")
	}
}
