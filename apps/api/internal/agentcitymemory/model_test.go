package agentcitymemory

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testOwner = "28000000-0000-4000-8000-000000000001"
const testAgent = "28000000-0000-4000-8000-000000000002"
const testMemory = "28000000-0000-4000-8000-000000000003"

func testNow() time.Time { return time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC) }
func testInput(k Kind) PutDeclarationInput {
	return PutDeclarationInput{CityID: "aberdeen-gb", Kind: k, Visibility: agentmemory.VisibilityPrivate, ValidUntil: testNow().Add(time.Hour)}
}
func testRecord(t *testing.T, k Kind) agentmemory.Record {
	t.Helper()
	in, e := NewMemoryInput(testInput(k), testNow())
	if e != nil {
		t.Fatal(e)
	}
	r, e := agentmemory.NewExplicit(testMemory, testAgent, actorref.PrincipalRef{Type: actorref.Person, ID: testOwner}, 1, in, testNow(), testNow())
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func testProjection() Projection {
	at := testNow()
	return Projection{SchemaVersion: Schema, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: testOwner}, AgentID: testAgent, CityID: "aberdeen-gb", AuthorityDigest: strings.Repeat("a", 64), TargetDigest: strings.Repeat("b", 64), Signals: []Signal{}, ObservedAt: at, ExpiresAt: at.Add(time.Minute), Verification: "UNAVAILABLE", HistoricalDates: "NOT_PROVIDED", ProcessingStatus: "UNAVAILABLE"}
}
func testSignal(k Kind) Signal {
	at, until := testNow(), testNow().Add(time.Hour)
	s := Signal{Kind: k, Basis: SelfDeclaration, Explanation: Explanation(k), Source: Source{Kind: MemorySource, ID: testMemory, Revision: 1}, RecordCreatedAt: at, SourceUpdatedAt: &at, ValidUntil: &until, Visibility: agentmemory.VisibilityPrivate}
	if k == Current {
		s.Source = Source{Kind: ContextSource, ID: testMemory, Token: strings.Repeat("c", 64)}
		s.SourceUpdatedAt = nil
		s.ValidUntil = nil
	}
	return s
}
func TestCityMemoryFourKindsAndNativeDeclarationShape(t *testing.T) {
	got := Kinds()
	got[0] = Visited
	if Kinds()[0] != Current {
		t.Fatal("registry aliased")
	}
	for _, k := range Kinds() {
		t.Run(string(k), func(t *testing.T) {
			p := testProjection()
			p.Signals = []Signal{testSignal(k)}
			p.SnapshotID = SnapshotID(p)
			if e := Validate(p, testNow()); e != nil {
				t.Fatal(e)
			}
			if k == Current {
				if _, e := NewMemoryInput(testInput(k), testNow()); !errors.Is(e, ErrInvalid) {
					t.Fatal("current duplicate Memory allowed")
				}
				return
			}
			r := testRecord(t, k)
			d, e := DecodeDeclaration(r)
			if e != nil || d.Kind != k || d.CityID != "aberdeen-gb" || d.Basis != SelfDeclaration || r.SourceType != agentmemory.SourceExplicit || r.Confidence != 1 || !ValidDeclarationKey(r.MemoryKey) {
				t.Fatal("explicit native shape", e)
			}
		})
	}
	if DeclarationKey("aberdeen-gb", Lived) == DeclarationKey("aberdeen-gb", Visited) || DeclarationKey("london-gb", Visited) == DeclarationKey("aberdeen-gb", Visited) {
		t.Fatal("city/category address collision")
	}
}
func TestCityMemoryStrictInputLimits(t *testing.T) {
	cases := map[string]func(*PutDeclarationInput){
		"current": func(p *PutDeclarationInput) { p.Kind = Current }, "home": func(p *PutDeclarationInput) { p.Kind = "HOME" }, "past": func(p *PutDeclarationInput) { p.Kind = "PAST" }, "destination": func(p *PutDeclarationInput) { p.Kind = "DESTINATION" }, "unknown": func(p *PutDeclarationInput) { p.Kind = "VERIFIED_VISIT" }, "empty_city": func(p *PutDeclarationInput) { p.CityID = "" }, "upper_city": func(p *PutDeclarationInput) { p.CityID = "London" }, "pad_city": func(p *PutDeclarationInput) { p.CityID = " aberdeen-gb" }, "long_city": func(p *PutDeclarationInput) { p.CityID = strings.Repeat("a", 101) }, "city_url": func(p *PutDeclarationInput) { p.CityID = "https://city" }, "negative_version": func(p *PutDeclarationInput) { p.ExpectedVersion = -1 }, "overflow": func(p *PutDeclarationInput) { p.ExpectedVersion = math.MaxInt64 }, "public": func(p *PutDeclarationInput) { p.Visibility = "PUBLIC" }, "zero_deadline": func(p *PutDeclarationInput) { p.ValidUntil = time.Time{} }, "deadline_now": func(p *PutDeclarationInput) { p.ValidUntil = testNow() }, "too_long": func(p *PutDeclarationInput) { p.ValidUntil = testNow().Add(agentmemory.MaxValidity + time.Microsecond) }, "precision_to_now": func(p *PutDeclarationInput) { p.ValidUntil = testNow().Add(time.Nanosecond) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			in := testInput(Lived)
			change(&in)
			r, e := NormalizePut(in, testNow())
			if !errors.Is(e, ErrInvalid) || r != (PutDeclarationInput{}) {
				t.Fatal("invalid input not zero", e)
			}
		})
	}
	in := testInput(Interested)
	in.ValidUntil = testNow().Add(agentmemory.MaxValidity)
	if _, e := NormalizePut(in, testNow()); e != nil {
		t.Fatal("inclusive max deadline", e)
	}
	if _, e := NormalizePut(in, time.Time{}); !errors.Is(e, ErrInvalid) {
		t.Fatal("zero clock")
	}
	for _, key := range []string{"", "city.v1.bad.visited", "city.v1." + strings.Repeat("a", 64) + ".current", "City.v1." + strings.Repeat("a", 64) + ".lived"} {
		if ValidDeclarationKey(key) {
			t.Fatal("bad control key")
		}
	}
}
func TestCityMemoryDeclarationRejectsNonAuthority(t *testing.T) {
	cases := map[string]func(*agentmemory.Record){"generic": func(r *agentmemory.Record) { r.StructuredValue = json.RawMessage(`{"visited":true}`) }, "profile_type": func(r *agentmemory.Record) { r.MemoryType = agentmemory.TypeHistory }, "wrong_key": func(r *agentmemory.Record) { r.MemoryKey = "generic.city" }, "wrong_summary": func(r *agentmemory.Record) { r.Summary = "已经核验居住" }, "inferred": func(r *agentmemory.Record) {
		r.SourceType = agentmemory.SourceInferred
		r.Status = agentmemory.StatusPendingReview
	}, "deleted": func(r *agentmemory.Record) {
		r.Status = agentmemory.StatusDeleted
		r.Summary = ""
		r.StructuredValue = json.RawMessage(`{}`)
	}, "unknown_field": func(r *agentmemory.Record) {
		r.StructuredValue = json.RawMessage(`{"schemaVersion":"agent.city_declaration.v1","cityId":"aberdeen-gb","kind":"LIVED","basis":"SELF_DECLARATION","verified":true}`)
	}, "repeat_key": func(r *agentmemory.Record) {
		r.StructuredValue = json.RawMessage(`{"schemaVersion":"agent.city_declaration.v1","cityId":"aberdeen-gb","cityId":"aberdeen-gb","kind":"LIVED","basis":"SELF_DECLARATION"}`)
	}, "current": func(r *agentmemory.Record) {
		r.StructuredValue = json.RawMessage(`{"schemaVersion":"agent.city_declaration.v1","cityId":"aberdeen-gb","kind":"CURRENT","basis":"SELF_DECLARATION"}`)
	}}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			r := testRecord(t, Lived)
			change(&r)
			d, e := DecodeDeclaration(r)
			if !errors.Is(e, ErrInvalid) || d != (Declaration{}) {
				t.Fatal("not rejected", e)
			}
		})
	}
}
func TestCityMemoryFiniteProjectionAndSeparation(t *testing.T) {
	cases := map[string]func(*Projection){"wrong_schema": func(p *Projection) { p.SchemaVersion = "profile" }, "org": func(p *Projection) { p.Owner.Type = actorref.Organization }, "cross_shape": func(p *Projection) { p.Owner.ID = "owner" }, "agent": func(p *Projection) { p.AgentID = "agent" }, "city": func(p *Projection) { p.CityID = "" }, "nil_signals": func(p *Projection) { p.Signals = nil }, "probability": func(p *Projection) { p.Verification = "VERIFIED" }, "dates": func(p *Projection) { p.HistoricalDates = "1999-01-01" }, "machine": func(p *Projection) { p.ProcessingStatus = "AVAILABLE" }, "token": func(p *Projection) { p.AuthorityDigest = "client" }, "future": func(p *Projection) { p.ObservedAt = testNow().Add(time.Hour) }, "long_lease": func(p *Projection) { p.ExpiresAt = p.ObservedAt.Add(MaxReadLease + time.Nanosecond) }, "empty_lease": func(p *Projection) { p.ExpiresAt = p.ObservedAt }, "bad_fixed_label": func(p *Projection) { p.Signals[0].Explanation = "已证实到访" }, "unknown_signal": func(p *Projection) { p.Signals[0].Kind = "PAST" }, "duplicate": func(p *Projection) { p.Signals = append(p.Signals, p.Signals[0]) }, "native_fake_revision": func(p *Projection) { p.Signals[0].Source.Revision = 1 }, "native_fake_updated": func(p *Projection) { at := testNow(); p.Signals[0].SourceUpdatedAt = &at }, "native_fake_until": func(p *Projection) { at := testNow().Add(time.Hour); p.Signals[0].ValidUntil = &at }, "native_public": func(p *Projection) { p.Signals[0].Visibility = "PUBLIC" }, "created_future": func(p *Projection) { p.Signals[0].RecordCreatedAt = testNow().Add(time.Hour) }, "history_wrong_source": func(p *Projection) { p.Signals = []Signal{testSignal(Lived)}; p.Signals[0].Source.Kind = ContextSource }, "history_fake_token": func(p *Projection) {
		p.Signals = []Signal{testSignal(Lived)}
		p.Signals[0].Source.Token = strings.Repeat("c", 64)
	}, "history_expiry": func(p *Projection) {
		p.Signals = []Signal{testSignal(Lived)}
		v := testNow().Add(time.Second)
		p.Signals[0].ValidUntil = &v
	}, "history_missing_update": func(p *Projection) { p.Signals = []Signal{testSignal(Lived)}; p.Signals[0].SourceUpdatedAt = nil }, "history_old_update": func(p *Projection) {
		p.Signals = []Signal{testSignal(Lived)}
		v := testNow().Add(-time.Second)
		p.Signals[0].SourceUpdatedAt = &v
	}, "history_zero_revision": func(p *Projection) { p.Signals = []Signal{testSignal(Lived)}; p.Signals[0].Source.Revision = 0 }}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			p := testProjection()
			p.Signals = []Signal{testSignal(Current)}
			change(&p)
			p.SnapshotID = SnapshotID(p)
			if !errors.Is(Validate(p, testNow()), ErrInvalid) {
				t.Fatal("invalid projection accepted")
			}
		})
	}
	p := testProjection()
	p.SnapshotID = SnapshotID(p)
	if !errors.Is(Validate(p, p.ExpiresAt), ErrExpired) {
		t.Fatal("expired lease accepted")
	}
	p.ExpiresAt = p.ExpiresAt.Add(time.Minute)
	if p.SnapshotID != SnapshotID(p) {
		t.Fatal("lease renewal contaminated source identity")
	}
	p.TargetDigest = strings.Repeat("f", 64)
	if p.SnapshotID == SnapshotID(p) {
		t.Fatal("changed current target did not invalidate")
	}
	p = testProjection()
	p.SnapshotID = SnapshotID(p)
	raw, _ := json.Marshal(p)
	var dst Projection
	if !errors.Is(json.Unmarshal(raw, &dst), ErrAuthorityJSON) || !reflect.DeepEqual(dst, Projection{}) {
		t.Fatal("JSON authority injected")
	}
}

func TestCityMemoryIssuedLeaseAndUTCYearBoundary(t *testing.T) {
	p := testProjection()
	p.SnapshotID = SnapshotID(p)
	if !errors.Is(ValidateIssued(p), ErrInvalid) {
		t.Fatal("plain typed shape became issued source")
	}
	issued, e := IssueProjection(p)
	if e != nil || ValidateIssued(issued) != nil {
		t.Fatal("server display receipt", e)
	}
	for _, field := range []string{"observed", "expires", "snapshot", "source"} {
		t.Run(field, func(t *testing.T) {
			v := issued
			switch field {
			case "observed":
				v.ObservedAt = v.ObservedAt.Add(time.Second)
			case "expires":
				v.ExpiresAt = v.ExpiresAt.Add(time.Second)
			case "snapshot":
				v.SnapshotID = strings.Repeat("f", 64)
			case "source":
				v.TargetDigest = strings.Repeat("f", 64)
				v.SnapshotID = SnapshotID(v)
			}
			if !errors.Is(ValidateIssued(v), ErrInvalid) {
				t.Fatal("issued display fields were silently renewed")
			}
		})
	}
	for name, at := range map[string]time.Time{
		"utc_before_year_one": time.Date(1, 1, 1, 0, 30, 0, 0, time.FixedZone("east", 3600)),
		"utc_after_year9999":  time.Date(9999, 12, 31, 23, 30, 0, 0, time.FixedZone("west", -3600)),
	} {
		t.Run(name, func(t *testing.T) {
			if validTime(at) {
				t.Fatal("local year hid invalid UTC year")
			}
			in := testInput(Lived)
			in.ValidUntil = at
			if _, e := NormalizePut(in, testNow()); !errors.Is(e, ErrInvalid) {
				t.Fatal("invalid canonical UTC time accepted")
			}
		})
	}
}

func TestCityMemoryClockValidationNeverClampsOrRenewsIssuedLease(t *testing.T) {
	// Deterministic shape boundary, not evidence that PG clock regressed in
	// the original native whole-suite failure. All native reads still use PG.
	p := testProjection()
	p.SnapshotID = SnapshotID(p)
	issued, err := IssueProjection(p)
	if err != nil {
		t.Fatal(err)
	}
	original := issued
	for _, tc := range []struct {
		name string
		now  time.Time
		want error
	}{
		{"before_observation", p.ObservedAt.Add(-time.Microsecond), ErrInvalid},
		{"at_observation", p.ObservedAt, nil},
		{"before_expiry", p.ExpiresAt.Add(-time.Microsecond), nil},
		{"at_expiry", p.ExpiresAt, ErrExpired},
		{"after_expiry", p.ExpiresAt.Add(time.Microsecond), ErrExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Validate(issued, tc.now); !errors.Is(got, tc.want) {
				t.Fatalf("clock boundary: got %v want %v", got, tc.want)
			}
			if !reflect.DeepEqual(issued, original) || ValidateIssued(issued) != nil {
				t.Fatal("validation silently changed source or issued lease")
			}
		})
	}
}
