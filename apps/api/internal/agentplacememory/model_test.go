package agentplacememory

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
)

const testPlace = "31000000-0000-4000-8000-000000000001"
const testOwner = "31000000-0000-4000-8000-000000000002"
const testAgent = "31000000-0000-4000-8000-000000000003"
const testSource = "31000000-0000-4000-8000-000000000004"

func nowFixture() time.Time { return time.Date(2026, 10, 3, 6, 0, 0, 0, time.UTC) }
func putFixture() PutDeclarationInput {
	return PutDeclarationInput{PlaceID: testPlace, Kind: Liked, Visibility: agentmemory.VisibilityPrivate, ValidUntil: nowFixture().Add(time.Hour)}
}
func projectionFixture() Projection {
	at := nowFixture()
	v, _ := agentevent.SnapshotVersion(agentevent.SavedPlaceSource, agentevent.CreatedAtDigestVersion, at.Add(-time.Hour), []byte(`{"id":"saved"}`))
	p := Projection{SchemaVersion: Schema, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: testOwner}, AgentID: testAgent, PlaceID: testPlace, CityID: "aberdeen-gb", AuthorityDigest: strings.Repeat("a", 64), TargetDigest: strings.Repeat("b", 64), Signals: []Signal{{Kind: Saved, Basis: CurrentBookmark, Source: Source{BookmarkSource, testSource, v}, RecordCreatedAt: at.Add(-time.Hour), SourceUpdatedAt: at.Add(-time.Hour), Visibility: agentmemory.VisibilityPrivate}}, ObservedAt: at, ExpiresAt: at.Add(MaxReadLease), VerifiedVisit: "UNAVAILABLE", Attendance: "UNAVAILABLE", ProcessingStatus: "UNAVAILABLE"}
	p.SnapshotID = SnapshotID(p)
	return p
}
func TestPlaceMemoryExplicitDeclarations(t *testing.T) {
	for _, kind := range []Kind{Liked, Visited} {
		for _, visibility := range []agentmemory.Visibility{agentmemory.VisibilityPrivate, agentmemory.VisibilityAgentOnly} {
			t.Run(string(kind)+"_"+string(visibility), func(t *testing.T) {
				in := putFixture()
				in.Kind = kind
				in.Visibility = visibility
				body, _ := json.Marshal(in)
				decoded, e := DecodePut(body, nowFixture())
				if e != nil || !reflect.DeepEqual(decoded, in) {
					t.Fatal("valid direct statement rejected", e)
				}
				memory, e := NewMemoryInput(in, "aberdeen-gb", nowFixture())
				if e != nil || memory.MemoryType != agentmemory.TypePlace || memory.Summary != DeclarationSummary(kind) {
					t.Fatal("statement incorrectly mapped")
				}
				r, e := agentmemory.NewExplicit(testSource, testAgent, actorref.PrincipalRef{Type: actorref.Person, ID: testOwner}, 1, memory, nowFixture(), nowFixture())
				if e != nil {
					t.Fatal(e)
				}
				v, e := DecodeDeclaration(r)
				if e != nil || v.Kind != kind || v.Basis != SelfDeclaration || v.PlaceID != testPlace {
					t.Fatal("direct Memory declaration was lost", e)
				}
				if kind == Visited && !strings.Contains(r.Summary, "未经核验") {
					t.Fatal("self-reported visit became verified")
				}
			})
		}
	}
}
func TestPlaceMemoryPutRejectsUnsupportedClaims(t *testing.T) {
	cases := []struct {
		name   string
		change func(*PutDeclarationInput)
	}{
		{"saved_not_declaration", func(p *PutDeclarationInput) { p.Kind = Saved }},
		{"moment_not_declaration", func(p *PutDeclarationInput) { p.Kind = CreatedMomentAt }},
		{"attendance_unavailable", func(p *PutDeclarationInput) { p.Kind = AttendedActivityAt }},
		{"unknown", func(p *PutDeclarationInput) { p.Kind = "CHECKED_IN" }},
		{"bad_place", func(p *PutDeclarationInput) { p.PlaceID = "aberdeen-gb" }},
		{"zero_uuid", func(p *PutDeclarationInput) { p.PlaceID = "00000000-0000-0000-0000-000000000000" }},
		{"case_id", func(p *PutDeclarationInput) { p.PlaceID = "31000000-0000-4000-8000-0000000000AA" }},
		{"spaces_id", func(p *PutDeclarationInput) { p.PlaceID = " " + testPlace }},
		{"negative_version", func(p *PutDeclarationInput) { p.ExpectedVersion = -1 }},
		{"exhausted_version", func(p *PutDeclarationInput) { p.ExpectedVersion = math.MaxInt64 }},
		{"public", func(p *PutDeclarationInput) { p.Visibility = "PUBLIC" }},
		{"expired", func(p *PutDeclarationInput) { p.ValidUntil = nowFixture() }},
		{"too_long", func(p *PutDeclarationInput) { p.ValidUntil = nowFixture().Add(agentmemory.MaxValidity + time.Second) }},
		{"no_deadline", func(p *PutDeclarationInput) { p.ValidUntil = time.Time{} }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := putFixture()
			c.change(&in)
			got, e := NormalizePut(in, nowFixture())
			if !errors.Is(e, ErrInvalid) || got != (PutDeclarationInput{}) {
				t.Fatal("invalid statement accepted", e)
			}
		})
	}
}
func TestPlaceMemoryStrictFlatInput(t *testing.T) {
	raw, _ := json.Marshal(putFixture())
	good := string(raw)
	cases := map[string]string{
		"confirmed":  strings.TrimSuffix(good, "}") + `,"confirmed":true}`,
		"purpose":    strings.TrimSuffix(good, "}") + `,"purpose":"MODEL_CONTEXT"}`,
		"owner":      strings.TrimSuffix(good, "}") + `,"ownerId":"` + testOwner + `"}`,
		"confidence": strings.TrimSuffix(good, "}") + `,"confidence":1}`,
		"duplicate":  strings.TrimSuffix(good, "}") + `,"kind":"VISITED"}`,
		"upper":      strings.Replace(good, `"kind":`, `"Kind":`, 1),
		"null":       strings.Replace(good, `"kind":"LIKED"`, `"kind":null`, 1),
		"nested":     strings.Replace(good, `"kind":"LIKED"`, `"kind":{"value":"LIKED"}`, 1),
		"array":      strings.Replace(good, `"kind":"LIKED"`, `"kind":["LIKED"]`, 1),
		"missing":    strings.Replace(good, `"expectedVersion":0,`, "", 1),
		"trailing":   good + good, "oversize": strings.Repeat(" ", 2049) + good,
	}
	for n, v := range cases {
		t.Run(n, func(t *testing.T) {
			if got, e := DecodePut([]byte(v), nowFixture()); !errors.Is(e, ErrInvalid) || got != (PutDeclarationInput{}) {
				t.Fatal("wire claim accepted", e)
			}
		})
	}
}
func TestPlaceMemoryNoGenericMemoryPromotion(t *testing.T) {
	in, _ := NewMemoryInput(putFixture(), "aberdeen-gb", nowFixture())
	r, _ := agentmemory.NewExplicit(testSource, testAgent, actorref.PrincipalRef{Type: actorref.Person, ID: testOwner}, 1, in, nowFixture(), nowFixture())
	mutations := map[string]func(*agentmemory.Record){
		"free_json": func(r *agentmemory.Record) {
			r.StructuredValue = json.RawMessage(`{"placeId":"` + testPlace + `","visited":true}`)
		},
		"inferred": func(r *agentmemory.Record) {
			r.SourceType = agentmemory.SourceInferred
			r.Status = agentmemory.StatusPendingReview
		},
		"wrong_type": func(r *agentmemory.Record) { r.MemoryType = agentmemory.TypeActivity },
		"wrong_key":  func(r *agentmemory.Record) { r.MemoryKey = "ordinary.preference" },
		"false_verification": func(r *agentmemory.Record) {
			r.StructuredValue = json.RawMessage(strings.Replace(string(r.StructuredValue), "SELF_DECLARATION", "VERIFIED_VISIT", 1))
		},
		"unknown_field": func(r *agentmemory.Record) {
			r.StructuredValue = json.RawMessage(strings.TrimSuffix(string(r.StructuredValue), "}") + `,"confirmed":true}`)
		},
		"private_body": func(r *agentmemory.Record) { r.Summary = "任意私密正文不能成为地点声明" },
		"deleted": func(r *agentmemory.Record) {
			r.Status = agentmemory.StatusDeleted
			r.Summary = ""
			r.StructuredValue = json.RawMessage(`{}`)
		},
	}
	for n, m := range mutations {
		t.Run(n, func(t *testing.T) {
			copy := r
			m(&copy)
			if got, e := DecodeDeclaration(copy); !errors.Is(e, ErrInvalid) || got != (Declaration{}) {
				t.Fatal("generic/unsupported Memory promoted to Place fact", e)
			}
		})
	}
}
func TestPlaceMemoryProjectionMetadataLease(t *testing.T) {
	p := projectionFixture()
	if e := Validate(p, nowFixture()); e != nil {
		t.Fatal(e)
	}
	id := p.SnapshotID
	p.ObservedAt = p.ObservedAt.Add(time.Second)
	p.ExpiresAt = p.ExpiresAt.Add(time.Second)
	if SnapshotID(p) != id {
		t.Fatal("read times changed source/authority identity")
	}
	p = projectionFixture()
	if !errors.Is(Validate(p, p.ExpiresAt), ErrExpired) {
		t.Fatal("old lease renewed")
	}
	raw, _ := json.Marshal(p)
	var fromJSON Projection
	if e := json.Unmarshal(raw, &fromJSON); !errors.Is(e, ErrAuthorityJSON) || !reflect.DeepEqual(fromJSON, Projection{}) {
		t.Fatal("JSON manufactured authority")
	}
}
func TestPlaceMemoryProjectionRejectsFalseFacts(t *testing.T) {
	cases := map[string]func(*Projection){
		"org":                  func(p *Projection) { p.Owner.Type = actorref.Organization },
		"body_as_digest":       func(p *Projection) { p.TargetDigest = "私密正文" },
		"visited_from_save":    func(p *Projection) { p.Signals[0].Kind = Visited },
		"liked_from_save":      func(p *Projection) { p.Signals[0].Kind = Liked },
		"attendance_from_save": func(p *Projection) { p.Signals[0].Kind = AttendedActivityAt },
		"unknown_source":       func(p *Projection) { p.Signals[0].Source.Kind = "ACTIVITY" },
		"invented_version": func(p *Projection) {
			p.Signals[0].Source.Version = agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: 1}
		},
		"verified":      func(p *Projection) { p.VerifiedVisit = "VERIFIED" },
		"attended":      func(p *Projection) { p.Attendance = "CONFIRMED" },
		"runtime_on":    func(p *Projection) { p.ProcessingStatus = "ACTIVE" },
		"duplicate":     func(p *Projection) { p.Signals = append(p.Signals, p.Signals[0]) },
		"nil_signals":   func(p *Projection) { p.Signals = nil },
		"long_lease":    func(p *Projection) { p.ExpiresAt = p.ObservedAt.Add(MaxReadLease + time.Second) },
		"future_record": func(p *Projection) { p.Signals[0].SourceUpdatedAt = p.ObservedAt.Add(time.Hour) },
	}
	for n, m := range cases {
		t.Run(n, func(t *testing.T) {
			p := projectionFixture()
			m(&p)
			p.SnapshotID = SnapshotID(p)
			if e := Validate(p, nowFixture()); !errors.Is(e, ErrInvalid) {
				t.Fatal("false current proof accepted", e)
			}
		})
	}
}
